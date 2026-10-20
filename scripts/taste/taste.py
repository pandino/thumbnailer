#!/usr/bin/env python3
"""Learn which movies get deleted or kept, from the frames of their thumbnail grids.

Runs on a desktop that is only sometimes on; the thumbnailer server (no GPU, weak CPU) only
stores what this worker produces. Each run:

1. embeds grids the server has no embedding for yet: every grid is cut into its tiles (the
   layout comes from the server), blank tiles are dropped, each frame is embedded with a
   SigLIP image model and the frame embeddings are averaged;
2. rebuilds the training set from all embeddings and the server's decision log, and trains
   two logistic regressions: "delete from the grid" (slideshow) and "archive after watching";
3. scores every live thumbnail and uploads the scores, which the slideshow uses even when
   this machine is off.

Embedding is time-boxed (--max-minutes), so the first pass over a large library is spread
over several runs. Training and scoring take seconds.

Requires the packages in requirements.txt (torch, open_clip_torch, scikit-learn, pillow,
numpy); the pure helpers at the top only need the standard library.
"""

import argparse
import fcntl
import io
import json
import logging
import os
import signal
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone

# Grid tiles are 320x180, so a 256 px model sees them at about native size; the 384 px variant
# only upscales them and is 2.5x slower on CPU
DEFAULT_ARCH = "ViT-B-16-SigLIP2-256"
DEFAULT_PRETRAINED = "webli"
# Frames per grid to embed, evenly spaced through the movie (a grid has 32; 16 halves the time)
DEFAULT_MAX_FRAMES = 16
PAGE_SIZE = 500
UPLOAD_BATCH = 16

# A tile whose grey levels vary less than this is blank (padding of short movies, black frames)
BLANK_TILE_STDDEV = 6.0

# Minimum examples per class before a model is trained and its scores used
MIN_PER_CLASS = {"genre": 10, "watch": 20}

# What label 0 and label 1 mean for each model
CLASS_NAMES = {"genre": ("keep", "delete"), "watch": ("delete", "archive")}

log = logging.getLogger("taste")


# --- pure helpers (standard library only) -----------------------------------

def tile_boxes(geometry):
    """Return the (left, top, right, bottom) crop box of every tile of a grid, row by row.

    geometry is the server's GridGeometry: cols, rows, tile_width, tile_height and padding
    (used both between tiles and as the outer margin). An unknown layout (cols == 0) has no
    tiles: the caller then embeds the whole image.
    """
    cols, rows = geometry.get("cols", 0), geometry.get("rows", 0)
    tw, th, pad = geometry.get("tile_width", 0), geometry.get("tile_height", 0), geometry.get("padding", 0)
    if cols <= 0 or rows <= 0 or tw <= 0 or th <= 0:
        return []
    boxes = []
    for r in range(rows):
        for c in range(cols):
            left = pad + c * (tw + pad)
            top = pad + r * (th + pad)
            boxes.append((left, top, left + tw, top + th))
    return boxes


def spread(items, n):
    """Pick n items evenly spaced over the list (all of them if there are n or fewer)."""
    if n <= 0 or len(items) <= n:
        return list(items)
    step = len(items) / n
    return [items[int(i * step)] for i in range(n)]


def split_unknown_layout(items, include_unknown):
    """Return (items to embed, number skipped) — items whose tile layout is unknown are skipped
    unless include_unknown is set."""
    keep, skipped = [], 0
    for item in items:
        if tile_boxes(item.get("geometry", {})) or include_unknown:
            keep.append(item)
        else:
            skipped += 1
    return keep, skipped


def final_actions(decisions):
    """Reduce the decision log to the standing action per (thumbnail, stage).

    Decisions are applied in log order; "undo" cancels the previous decision of the same
    thumbnail and stage. Returns {(thumbnail_id, stage): action}.
    """
    stacks = {}
    for d in sorted(decisions, key=lambda d: d["id"]):
        key = (d["thumbnail_id"], d["stage"])
        stack = stacks.setdefault(key, [])
        if d["action"] == "undo":
            if stack:
                stack.pop()
        else:
            stack.append(d["action"])
    return {key: stack[-1] for key, stack in stacks.items() if stack}


def derive_labels(decisions):
    """Return training labels {"genre": {id: 0/1}, "watch": {id: 0/1}} from the decision log.

    genre (judged from the grid): deleted in the slideshow = 1; passed or archived in the
    slideshow, or decided in the player (it made it past the slideshow) = 0.
    watch (judged after watching): archived in the player = 1, deleted in the player = 0.
    Skipped thumbnails have no decision and therefore no label.
    """
    finals = final_actions(decisions)
    genre, watch = {}, {}
    for (tid, stage), action in finals.items():
        if stage == "slideshow":
            if action == "delete":
                genre[tid] = 1
            elif action in ("pass", "archive"):
                genre[tid] = 0
        elif stage == "player":
            if action in ("archive", "delete"):
                watch[tid] = 1 if action == "archive" else 0
                genre.setdefault(tid, 0)
    # A slideshow decision wins over the implied one from the player
    for (tid, stage), action in finals.items():
        if stage == "slideshow" and action == "delete":
            genre[tid] = 1
    return {"genre": genre, "watch": watch}


def judged_at(decisions, stage):
    """IDs of thumbnails with a standing decision at the given stage."""
    return {tid for (tid, s) in final_actions(decisions) if s == stage}


def class_counts(labels):
    ones = sum(1 for v in labels.values() if v == 1)
    return len(labels) - ones, ones


def label_summary(name, zeros, ones):
    """Describe a model's training set, e.g. "1704 keep / 145 delete"."""
    zero_name, one_name = CLASS_NAMES[name]
    return f"{zeros} {zero_name} / {ones} {one_name}"


# --- HTTP --------------------------------------------------------------------

def http_request(method, url, body=None, timeout=60):
    """Send a request and return the raw response body."""
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read()


def http_json(method, url, body=None, timeout=60):
    return json.loads(http_request(method, url, body, timeout))


class Server:
    """The thumbnailer's /api/v1/ml endpoints."""

    def __init__(self, base_url, timeout):
        self.base = base_url.rstrip("/")
        self.timeout = timeout

    def _pages(self, path, params, key):
        params = dict(params, limit=PAGE_SIZE, after_id=0)
        while True:
            page = http_json("GET", f"{self.base}{path}?{urllib.parse.urlencode(params)}", timeout=self.timeout)
            yield from page[key]
            if not page.get("next_after_id"):
                return
            params["after_id"] = page["next_after_id"]

    def items(self, missing_model=""):
        params = {"missing_model": missing_model} if missing_model else {}
        return self._pages("/api/v1/ml/items", params, "items")

    def embeddings(self, model):
        return self._pages("/api/v1/ml/embeddings", {"model": model}, "embeddings")

    def decisions(self):
        return self._pages("/api/v1/ml/decisions", {}, "decisions")

    def grid(self, grid_url):
        """Download a grid image, or return None if the server has no file for it (404).

        A few thumbnail rows have lost their grid file; they are skipped, not failures, so
        they don't make every timer run report an error.
        """
        try:
            return http_request("GET", self.base + grid_url, timeout=self.timeout)
        except urllib.error.HTTPError as e:
            if e.code == 404:
                return None
            raise

    def put_embeddings(self, model, embeddings):
        http_json("PUT", f"{self.base}/api/v1/ml/embeddings",
                  {"model": model, "embeddings": embeddings}, timeout=self.timeout)

    def put_scores(self, scores, meta):
        http_json("PUT", f"{self.base}/api/v1/ml/scores", {"scores": scores, "meta": meta}, timeout=self.timeout)


# --- embedding ---------------------------------------------------------------

def frames_of(image, geometry, max_frames=0):
    """Cut a grid into its non-blank tiles, at most max_frames of them evenly spaced (0 = all).

    Returns the whole image if the layout is unknown or every tile is blank.
    """
    from PIL import ImageStat

    image = image.convert("RGB")
    frames = []
    for box in tile_boxes(geometry):
        tile = image.crop(box)
        if ImageStat.Stat(tile.convert("L")).stddev[0] >= BLANK_TILE_STDDEV:
            frames.append(tile)
    return spread(frames, max_frames) or [image]


def resolve_device(requested, gpu_available):
    """Pick the torch device: "cpu", "cuda" (fails without a GPU) or "auto" (GPU if usable)."""
    if requested == "auto":
        return "cuda" if gpu_available else "cpu"
    if requested == "cuda" and not gpu_available:
        raise SystemExit("--device cuda: no usable GPU. On an RX 6700 XT (gfx1031) with a ROCm build "
                         "of torch, set HSA_OVERRIDE_GFX_VERSION=10.3.0")
    return requested


class Embedder:
    def __init__(self, arch, pretrained, threads, device):
        import open_clip
        import torch

        if threads:
            torch.set_num_threads(threads)
        self.torch = torch
        # ROCm builds of torch expose AMD GPUs as "cuda"
        self.device = resolve_device(device, torch.cuda.is_available())
        log.info("Loading %s/%s on %s", arch, pretrained, self.device)
        self.model, _, self.preprocess = open_clip.create_model_and_transforms(arch, pretrained=pretrained)
        self.model = self.model.to(self.device).eval()

    def embed(self, frames):
        """Mean of the L2-normalized frame embeddings, normalized again."""
        torch = self.torch
        with torch.no_grad():
            batch = torch.stack([self.preprocess(f) for f in frames]).to(self.device)
            features = self.model.encode_image(batch)
            features = features / features.norm(dim=-1, keepdim=True)
            mean = features.mean(dim=0)
            mean = mean / mean.norm()
        return mean.float().cpu().tolist()


# --- training ----------------------------------------------------------------

class CalibratedModel:
    """A ranking model plus a calibration of its scores into real probabilities.

    The ranking model is trained with balanced class weights, so the rare class isn't ignored.
    That weighting also inflates its probabilities: on production data a "60%" delete score was
    deleted about 15% of the time. A one-feature logistic regression (Platt scaling) on the
    ranking model's out-of-fold scores maps them back to observed frequencies. It is monotonic,
    so the ranking (and the slideshow orders) is unchanged.
    """

    def __init__(self, model, platt):
        self.model = model
        self.platt = platt

    def predict(self, X):
        logits = self.model.decision_function(X).reshape(-1, 1)
        return self.platt.predict_proba(logits)[:, 1]


def train(X, y, name, calibration_mask=None):
    """Fit a class-balanced logistic regression with cross-validated C, then calibrate it.

    calibration_mask selects the examples whose outcome frequencies the probabilities should
    match (e.g. movies judged in the slideshow, the population the badge is shown for); None
    means all. Returns (CalibratedModel, cv_auc, brier), or (None, None, None) when there are
    too few examples per class.
    """
    import numpy as np
    from sklearn.linear_model import LogisticRegression, LogisticRegressionCV
    from sklearn.model_selection import StratifiedKFold, cross_val_predict

    zeros, ones = int((y == 0).sum()), int((y == 1).sum())
    if min(zeros, ones) < MIN_PER_CLASS[name]:
        log.info("%s model: %s, need %d of each; not trained",
                 name, label_summary(name, zeros, ones), MIN_PER_CLASS[name])
        return None, None, None

    folds = min(5, zeros, ones)
    model = LogisticRegressionCV(Cs=np.logspace(-2, 2, 9), cv=folds, scoring="roc_auc",
                                 class_weight="balanced", max_iter=5000)
    model.fit(X, y)
    # Mean CV AUC of the chosen C
    auc = float(np.max(model.scores_[1].mean(axis=0)))

    # Out-of-fold scores of the same model, so the calibration sees honest, unseen-data scores
    cv = StratifiedKFold(folds, shuffle=True, random_state=0)
    same = LogisticRegression(C=model.C_[0], class_weight="balanced", max_iter=5000)
    logits = cross_val_predict(same, X, y, cv=cv, method="decision_function").reshape(-1, 1)
    mask = np.ones(len(y), dtype=bool) if calibration_mask is None else np.asarray(calibration_mask)
    if len(set(y[mask])) < 2:
        mask = np.ones(len(y), dtype=bool)
    platt = LogisticRegression(C=1e6, max_iter=1000).fit(logits[mask], y[mask])
    p = platt.predict_proba(logits[mask])[:, 1]
    brier = float(np.mean((p - y[mask]) ** 2))

    log.info("%s model: %s, C=%.3g, CV AUC %.3f, calibrated on %d examples (Brier %.4f)",
             name, label_summary(name, zeros, ones), model.C_[0], auc, int(mask.sum()), brier)
    return CalibratedModel(model, platt), auc, brier


class Worker:
    def __init__(self, args):
        self.args = args
        self.server = Server(args.server, args.timeout)
        # Embeddings are only comparable when made the same way, so the frame count is part of the key
        self.model_key = f"{args.arch}/{args.pretrained}/f{args.max_frames}"
        self.stop = False

    def embed_missing(self):
        """Embed grids without an embedding until done, stopped or out of time."""
        deadline = time.monotonic() + self.args.max_minutes * 60
        pending, unknown = split_unknown_layout(self.server.items(self.model_key), self.args.embed_unknown_layout)
        if unknown:
            # The server reports "not checked yet" and "checked, not a standard grid" the same way,
            # and an embedding is never recomputed, so don't embed them as whole images by default
            log.info("Skipping %d grids with an unknown tile layout (the server records layouts after "
                     "its next scan; --embed-unknown-layout embeds them as whole images)", unknown)
        if not pending:
            log.info("Nothing to embed")
            return 0
        log.info("%d grids to embed", len(pending))

        from PIL import Image

        embedder = Embedder(self.args.arch, self.args.pretrained, self.args.threads, self.args.device)
        batch, done, failed, missing = [], 0, 0, 0
        for item in pending:
            if self.stop or time.monotonic() > deadline:
                log.info("Stopping embedding (%s); the rest is left for the next run",
                         "interrupted" if self.stop else "time box reached")
                break
            try:
                data = self.server.grid(item["grid_url"])
                if data is None:
                    log.debug("Grid of %s is missing on the server, skipped", item["movie_filename"])
                    missing += 1
                    continue
                image = Image.open(io.BytesIO(data))
                vector = embedder.embed(frames_of(image, item["geometry"], self.args.max_frames))
            except (urllib.error.URLError, OSError) as e:
                log.warning("Cannot embed %s: %s", item["movie_filename"], e)
                failed += 1
                continue
            batch.append({"id": item["id"], "vector": vector})
            done += 1
            if len(batch) >= UPLOAD_BATCH:
                self.server.put_embeddings(self.model_key, batch)
                batch = []
                log.info("Embedded %d/%d", done, len(pending))
        if batch:
            self.server.put_embeddings(self.model_key, batch)
        log.info("Embedded %d grids (%d failed, %d missing on the server)", done, failed, missing)
        return failed

    def train_and_score(self):
        import numpy as np

        vectors = {e["id"]: e["vector"] for e in self.server.embeddings(self.model_key)}
        live = {item["id"] for item in self.server.items() if item["live"]}
        decisions = list(self.server.decisions())
        labels = derive_labels(decisions)
        # Probabilities are calibrated against the decisions of the stage each model predicts
        judged = {"genre": judged_at(decisions, "slideshow"), "watch": judged_at(decisions, "player")}
        log.info("%d embeddings, %d live grids", len(vectors), len(live))

        meta = {
            "model": self.model_key,
            "trained_at": datetime.now(timezone.utc).astimezone().strftime("%Y-%m-%d %H:%M"),
        }
        predictions = {}
        for name in ("genre", "watch"):
            ids = [tid for tid in labels[name] if tid in vectors]
            zeros, ones = class_counts({tid: labels[name][tid] for tid in ids})
            meta[f"{name}_labels"] = label_summary(name, zeros, ones)
            if not ids:
                continue
            X = np.array([vectors[tid] for tid in ids], dtype=np.float32)
            y = np.array([labels[name][tid] for tid in ids])
            model, auc, brier = train(X, y, name, [tid in judged[name] for tid in ids])
            if model is None:
                continue
            meta[f"{name}_auc"] = f"{auc:.2f}"
            meta[f"{name}_brier"] = f"{brier:.3f}"
            scored = sorted(tid for tid in live if tid in vectors)
            if scored:
                p = model.predict(np.array([vectors[tid] for tid in scored], dtype=np.float32))
                predictions[name] = dict(zip(scored, p.tolist()))

        if "genre" not in predictions:
            log.info("No delete model yet, scores not updated")
            return
        scores = []
        for tid, p_delete in predictions["genre"].items():
            score = {"id": tid, "p_delete": p_delete}
            if "watch" in predictions:
                score["p_archive"] = predictions["watch"][tid]
            scores.append(score)
        if self.args.dry_run:
            log.info("Dry run: would upload %d scores with %s", len(scores), meta)
            return
        self.server.put_scores(scores, meta)
        log.info("Uploaded %d scores", len(scores))

    def run(self):
        failed = 0
        if not self.args.no_embed:
            failed = self.embed_missing()
        if not self.stop:
            self.train_and_score()
        return 1 if failed else 0


# --- CLI -----------------------------------------------------------------------

def parse_args(argv):
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--server", required=True, help="thumbnailer base URL, e.g. http://192.168.1.35:8080")
    p.add_argument("--arch", default=DEFAULT_ARCH, help=f"open_clip model (default {DEFAULT_ARCH})")
    p.add_argument("--pretrained", default=DEFAULT_PRETRAINED, help=f"open_clip weights (default {DEFAULT_PRETRAINED})")
    p.add_argument("--max-frames", type=int, default=DEFAULT_MAX_FRAMES,
                   help=f"frames per grid to embed, 0 for all (default {DEFAULT_MAX_FRAMES})")
    p.add_argument("--max-minutes", type=float, default=20, help="time box for embedding per run (default 20)")
    p.add_argument("--device", choices=("cpu", "cuda", "auto"), default="cpu",
                   help="where to embed: cpu (default), cuda (GPU, also AMD with a ROCm build of torch) "
                        "or auto (GPU if usable)")
    p.add_argument("--threads", type=int, default=0, help="torch CPU threads (default: torch's choice)")
    p.add_argument("--timeout", type=float, default=60, help="HTTP timeout in seconds (default 60)")
    p.add_argument("--embed-unknown-layout", action="store_true",
                   help="embed grids with an unknown tile layout as whole images (default: skip them)")
    p.add_argument("--no-embed", action="store_true", help="only train and score")
    p.add_argument("--dry-run", action="store_true", help="embed, but only print the scores instead of uploading")
    p.add_argument("--lock", default=os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/tmp"), "thumbnailer-taste.lock"),
                   help="lock file preventing overlapping runs")
    p.add_argument("-v", "--verbose", action="store_true")
    return p.parse_args(argv)


def main(argv=None):
    args = parse_args(argv)
    logging.basicConfig(level=logging.DEBUG if args.verbose else logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s")

    lock = open(args.lock, "w")
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        log.info("Another run is in progress, exiting")
        return 0

    worker = Worker(args)

    def request_stop(signum, frame):
        if worker.stop:
            sys.exit(130)
        log.info("Stopping after the current grid (Ctrl-C again to abort)")
        worker.stop = True

    signal.signal(signal.SIGINT, request_stop)
    signal.signal(signal.SIGTERM, request_stop)
    try:
        return worker.run()
    except urllib.error.URLError as e:
        log.error("Thumbnailer not reachable at %s: %s", args.server, e)
        return 1


if __name__ == "__main__":
    sys.exit(main())
