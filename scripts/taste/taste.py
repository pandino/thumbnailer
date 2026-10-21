#!/usr/bin/env python3
"""Learn which movies end up deleted or archived, from the frames of their thumbnail grids.

Runs on a desktop that is only sometimes on; the thumbnailer server (no GPU, weak CPU) only
stores what this worker produces. Each run:

1. embeds grids the server has no embedding for yet: every grid is cut into its tiles (the
   layout comes from the server), blank tiles are dropped, each frame is embedded with a
   SigLIP image model and the frame embeddings are averaged;
2. rebuilds the training set from all embeddings and the server's decision log. Every decided
   movie has an outcome, deleted < undecided (passed in the slideshow, not watched yet) <
   archived, wherever the decision was made; two calibrated logistic regressions learn
   P(deleted) and P(archived) from it (an ordinal model, after Frank & Hall);
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

# Final outcomes of a movie, in order
DELETED, UNDECIDED, ARCHIVED = 0, 1, 2
OUTCOME_NAMES = {DELETED: "deleted", UNDECIDED: "undecided", ARCHIVED: "archived"}

# Minimum deleted and archived movies before the respective model is trained and its scores used
MIN_PER_CLASS = 10

# A delete made after watching counts this much in the delete model, relative to a slideshow
# delete: the grid looked worth watching, so it says less about what to delete on sight
DEFAULT_WATCHED_DELETE_WEIGHT = 0.5

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


def outcomes(decisions):
    """Return each decided movie's final outcome {thumbnail_id: DELETED|UNDECIDED|ARCHIVED}."""
    return {tid: outcome for tid, (outcome, _) in final_decisions(decisions).items()}


def final_decisions(decisions):
    """Return each decided movie's final outcome and the stage it was decided at
    {thumbnail_id: (DELETED|UNDECIDED|ARCHIVED, "slideshow"|"player")}.

    A standing delete or archive, in the slideshow or after watching, is final; when a movie
    has both (e.g. archived in the slideshow, deleted after watching) the later one wins. A
    standing slideshow pass without a later decision is UNDECIDED: the user wants to watch the
    movie before deciding. Skipped and undone decisions give no outcome.
    """
    stacks = {}
    for d in sorted(decisions, key=lambda d: d["id"]):
        stack = stacks.setdefault((d["thumbnail_id"], d["stage"]), [])
        if d["action"] == "undo":
            if stack:
                stack.pop()
        else:
            stack.append((d["id"], d["action"], d["stage"]))

    standing = {}
    for (tid, _), stack in stacks.items():
        if stack:
            standing.setdefault(tid, []).append(stack[-1])

    result = {}
    for tid, decided in standing.items():
        final = [x for x in decided if x[1] in ("delete", "archive")]
        if final:
            _, action, stage = max(final)
            result[tid] = (DELETED if action == "delete" else ARCHIVED, stage)
        elif any(a == "pass" for _, a, _ in decided):
            result[tid] = (UNDECIDED, "slideshow")
    return result


def delete_weights(final, ids, watched_delete_weight):
    """Training weights for the delete model: a delete made after watching (in the player) is a
    weaker "not interested" signal than a delete from the slideshow grid, so it counts less."""
    return [watched_delete_weight if final[tid] == (DELETED, "player") else 1.0 for tid in ids]


def outcome_summary(values):
    """Describe a training set, e.g. "146 deleted / 1210 undecided / 493 archived"."""
    values = list(values)
    return " / ".join(f"{values.count(o)} {OUTCOME_NAMES[o]}" for o in (DELETED, UNDECIDED, ARCHIVED))


def consistent(p_delete, p_archive):
    """The two models are fitted separately, so their probabilities can add up to more than 1;
    scale them down to a valid distribution over the three outcomes when they do."""
    total = p_delete + p_archive
    if total > 1:
        return p_delete / total, p_archive / total
    return p_delete, p_archive


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


def train(X, y, name, sample_weight=None):
    """Fit a class-balanced logistic regression for the binary target y (1 = the named outcome),
    with cross-validated C, then calibrate its probabilities on out-of-fold scores. sample_weight
    (default all 1) scales how much each example counts, in fitting and calibration alike.

    Returns (CalibratedModel, cv_auc, brier), or (None, None, None) when there are too few
    examples of either class.
    """
    import numpy as np
    from sklearn.linear_model import LogisticRegression, LogisticRegressionCV
    from sklearn.model_selection import StratifiedKFold, cross_val_predict

    w = np.ones(len(y)) if sample_weight is None else np.asarray(sample_weight, dtype=float)
    zeros, ones = int((y == 0).sum()), int((y == 1).sum())
    if min(zeros, ones) < MIN_PER_CLASS:
        log.info("%s model: %d %s, %d other, need %d of each; not trained",
                 name, ones, name, zeros, MIN_PER_CLASS)
        return None, None, None

    folds = min(5, zeros, ones)
    model = LogisticRegressionCV(Cs=np.logspace(-2, 2, 9), cv=folds, scoring="roc_auc",
                                 class_weight="balanced", max_iter=5000)
    model.fit(X, y, sample_weight=w)
    # Mean CV AUC of the chosen C
    auc = float(np.max(model.scores_[1].mean(axis=0)))

    # Out-of-fold scores of the same model, so the calibration sees honest, unseen-data scores
    cv = StratifiedKFold(folds, shuffle=True, random_state=0)
    same = LogisticRegression(C=model.C_[0], class_weight="balanced", max_iter=5000)
    logits = cross_val_predict(same, X, y, cv=cv, method="decision_function",
                               params={"sample_weight": w}).reshape(-1, 1)
    platt = LogisticRegression(C=1e6, max_iter=1000).fit(logits, y, sample_weight=w)
    brier = float(np.average((platt.predict_proba(logits)[:, 1] - y) ** 2, weights=w))

    log.info("%s model: %d %s, %d other, C=%.3g, CV AUC %.3f, Brier %.4f",
             name, ones, name, zeros, model.C_[0], auc, brier)
    return CalibratedModel(model, platt), auc, brier


class Worker:
    def __init__(self, args):
        self.args = args
        self.server = Server(args.server, args.timeout)
        # Embeddings are only comparable when made the same way, so the frame count is part of the key
        self.model_key = f"{args.arch}/{args.pretrained}/f{args.max_frames}"
        self.stop = False
        # True while grids are being embedded: a stop request then finishes the current grid and
        # uploads the batch; at any other time nothing is lost by exiting at once
        self.embedding = False

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

        embedder = Embedder(self.args.arch, self.args.pretrained, self.args.threads, self.args.device)
        self.embedding = True
        try:
            return self._embed(embedder, pending, deadline)
        finally:
            self.embedding = False

    def _embed(self, embedder, pending, deadline):
        from PIL import Image

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
        final = final_decisions(self.server.decisions())
        outcome = {tid: o for tid, (o, _) in final.items()}
        log.info("%d embeddings, %d live grids", len(vectors), len(live))

        ids = [tid for tid in outcome if tid in vectors]
        y = np.array([outcome[tid] for tid in ids])
        meta = {
            "model": self.model_key,
            "trained_at": datetime.now(timezone.utc).astimezone().strftime("%Y-%m-%d %H:%M"),
            "labels": outcome_summary(y.tolist()),
        }
        if not ids:
            log.info("No decided movies yet, scores not updated")
            return
        X = np.array([vectors[tid] for tid in ids], dtype=np.float32)

        scored = sorted(tid for tid in live if tid in vectors)
        X_live = np.array([vectors[tid] for tid in scored], dtype=np.float32)
        predictions = {}
        # Ordinal outcome as two binary models over all decided movies: P(deleted), P(archived)
        weights = {"delete": delete_weights(final, ids, self.args.watched_delete_weight), "archive": None}
        for name, target in (("delete", DELETED), ("archive", ARCHIVED)):
            model, auc, brier = train(X, (y == target).astype(int), name, weights[name])
            if model is None:
                continue
            meta[f"auc_{name}"] = f"{auc:.2f}"
            meta[f"brier_{name}"] = f"{brier:.3f}"
            if scored:
                predictions[name] = model.predict(X_live).tolist()

        if "delete" not in predictions:
            log.info("No delete model yet, scores not updated")
            return
        scores = []
        for i, tid in enumerate(scored):
            score = {"id": tid, "p_delete": predictions["delete"][i]}
            if "archive" in predictions:
                score["p_delete"], score["p_archive"] = consistent(score["p_delete"], predictions["archive"][i])
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


def sleep_unless_stopped(worker, seconds):
    """Sleep up to seconds, waking within a second once the worker is asked to stop."""
    end = time.monotonic() + seconds
    while not worker.stop and time.monotonic() < end:
        time.sleep(min(1.0, end - time.monotonic()))


def run_every(worker, minutes, wait=sleep_unless_stopped):
    """Run the worker every few minutes until it is stopped (--every, for a container).

    A failed run is logged and retried on the next cycle: the server is unreachable while it is
    redeployed, which must not stop the loop.
    """
    while True:
        try:
            worker.run()
        except Exception:
            log.exception("Run failed, retrying in %g min", minutes)
        if worker.stop:
            return 0
        wait(worker, minutes * 60)
        if worker.stop:
            return 0


# --- CLI -----------------------------------------------------------------------

def parse_args(argv):
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--server", required=True, help="thumbnailer base URL, e.g. http://192.168.1.35:8080")
    p.add_argument("--arch", default=DEFAULT_ARCH, help=f"open_clip model (default {DEFAULT_ARCH})")
    p.add_argument("--pretrained", default=DEFAULT_PRETRAINED, help=f"open_clip weights (default {DEFAULT_PRETRAINED})")
    p.add_argument("--watched-delete-weight", type=float, default=DEFAULT_WATCHED_DELETE_WEIGHT,
                   help="how much a delete made after watching counts in the delete model, relative to "
                        f"a slideshow delete (default {DEFAULT_WATCHED_DELETE_WEIGHT})")
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
    p.add_argument("--every", type=float, default=0, metavar="MINUTES",
                   help="keep running, starting a run every MINUTES (default 0: run once and exit)")
    p.add_argument("--no-embed", action="store_true", help="only train and score")
    p.add_argument("--dry-run", action="store_true", help="embed, but only print the scores instead of uploading")
    p.add_argument("--lock", default=os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/tmp"), "thumbnailer-taste.lock"),
                   help="lock file preventing overlapping runs")
    p.add_argument("-v", "--verbose", action="store_true")
    args = p.parse_args(argv)
    if not 0 < args.watched_delete_weight <= 1:
        p.error("--watched-delete-weight must be in (0, 1]")
    if args.every < 0:
        p.error("--every must not be negative")
    return args


def main(argv=None):
    args = parse_args(argv)
    if args.threads:
        # numpy/scikit-learn size their thread pools from the host's cores, not from a container's
        # CPU quota; set before they are first imported
        for var in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
            os.environ.setdefault(var, str(args.threads))
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
        worker.stop = True
        if not worker.embedding:
            log.info("Stopping")
            sys.exit(0)
        log.info("Stopping after the current grid (Ctrl-C again to abort)")

    signal.signal(signal.SIGINT, request_stop)
    signal.signal(signal.SIGTERM, request_stop)
    if args.every:
        log.info("Running every %g min", args.every)
        return run_every(worker, args.every)
    try:
        return worker.run()
    except urllib.error.URLError as e:
        log.error("Thumbnailer not reachable at %s: %s", args.server, e)
        return 1


if __name__ == "__main__":
    sys.exit(main())
