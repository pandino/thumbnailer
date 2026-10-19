#!/usr/bin/env python3
"""Tag movie thumbnails with a local vision model.

Pages through the thumbnailer's /api/v1/thumbnails listing, downloads each
thumbnail (a contact sheet of frames from one movie), asks an
OpenAI-compatible vision model (LM Studio, Ollama /v1, llama.cpp, ...) for
tags, and writes them back together with a marker tag so the image is not
analyzed again on the next run.

Only the Python standard library is used.
"""

import argparse
import base64
import json
import logging
import re
import signal
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

MAX_TAG_LENGTH = 64  # mirrors models.MaxTagLength on the server
PAGE_SIZE = 50

log = logging.getLogger("ai-tagger")


# --- tag handling -----------------------------------------------------------

def clean_tag(tag):
    """Normalize a tag like the server does; return None if it would be rejected."""
    if not isinstance(tag, str):
        return None
    normalized = " ".join(tag.split()).lower()
    if not normalized or len(normalized) > MAX_TAG_LENGTH or "," in normalized or "/" in normalized:
        return None
    return normalized


def clean_tags(tags):
    """Normalize a list of tags, dropping invalid ones and duplicates (order preserved)."""
    result = []
    for tag in tags:
        cleaned = clean_tag(tag)
        if cleaned and cleaned not in result:
            result.append(cleaned)
    return result


def load_keywords(keywords_arg, keywords_file):
    """Merge comma-separated CLI keywords and a one-per-line file (# comments allowed)."""
    raw = []
    if keywords_arg:
        raw.extend(keywords_arg.split(","))
    if keywords_file:
        with open(keywords_file, encoding="utf-8") as f:
            for line in f:
                line = line.split("#", 1)[0].strip()
                if line:
                    raw.append(line)
    return clean_tags(raw)


def select_tags(model_tags, keywords, keywords_only, max_tags, marker):
    """Clean the model's answer, apply strict keyword mode, cap the count and drop the marker."""
    tags = [t for t in clean_tags(model_tags) if t != marker]
    if keywords_only:
        allowed = set(keywords)
        tags = [t for t in tags if t in allowed]
    return tags[:max_tags]


# --- prompt / model ---------------------------------------------------------

def build_prompt(keywords, existing_tags, keywords_only, max_tags):
    """Build the instruction text; user keywords are listed before existing server tags."""
    lines = [
        "This image is a contact sheet: a grid of frames sampled from a single movie.",
        f"Describe the movie's content with at most {max_tags} short tags "
        "(lowercase, one to three words each, no commas or slashes).",
    ]
    if keywords:
        lines.append("Preferred tags, use them whenever they fit: " + ", ".join(keywords) + ".")
    others = [t for t in existing_tags if t not in keywords]
    if others and not keywords_only:
        lines.append("Tags already in use, reuse them instead of synonyms: " + ", ".join(others) + ".")
    if keywords_only:
        lines.append("Only use tags from the preferred list. If none fit, return an empty list.")
    else:
        lines.append("Only invent a new tag when nothing from the lists fits.")
    lines.append('Answer with JSON only, in the form {"tags": ["tag one", "tag two"]}.')
    return "\n".join(lines)


TAGS_SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "tags",
        "strict": True,
        "schema": {
            "type": "object",
            "properties": {"tags": {"type": "array", "items": {"type": "string"}}},
            "required": ["tags"],
            "additionalProperties": False,
        },
    },
}


def parse_model_answer(content):
    """Extract the tag list from the model's reply, tolerating code fences or surrounding text."""
    if not isinstance(content, str):
        raise ValueError("model reply has no text content")
    match = re.search(r"\{.*\}", content, re.DOTALL)
    if not match:
        raise ValueError(f"no JSON object in model reply: {content[:200]!r}")
    data = json.loads(match.group(0))
    tags = data.get("tags") if isinstance(data, dict) else None
    if not isinstance(tags, list):
        raise ValueError(f"model reply has no 'tags' list: {content[:200]!r}")
    return tags


# --- HTTP -------------------------------------------------------------------

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


class Tagger:
    def __init__(self, args, keywords):
        self.args = args
        self.server = args.server.rstrip("/")
        self.api_url = args.api_url.rstrip("/")
        self.keywords = keywords
        self.model = args.model
        self.stop = False

    def resolve_model(self):
        """Use --model, or the first model the API reports as available."""
        if self.model:
            return
        models = http_json("GET", self.api_url + "/models", timeout=30).get("data") or []
        if not models:
            raise SystemExit(f"no models available at {self.api_url}; load one or pass --model")
        self.model = models[0]["id"]
        log.info("Using model %s", self.model)

    def existing_tags(self):
        if self.args.no_existing_tags:
            return []
        counts = http_json("GET", self.server + "/api/tags")
        return [c["tag"] for c in counts if c["tag"] != self.args.marker]

    def pages(self):
        """Yield thumbnails still lacking the marker, one page at a time."""
        params = {"without_tag": self.args.marker, "limit": PAGE_SIZE}
        if self.args.unviewed_only:
            params["viewed"] = "0"
        if self.args.untagged_only:
            params["untagged"] = "true"
        after_id = 0
        while not self.stop:
            params["after_id"] = after_id
            page = http_json("GET", self.server + "/api/v1/thumbnails?" + urllib.parse.urlencode(params))
            yield from page["items"]
            after_id = page["next_after_id"]
            if not after_id:
                return

    def ask_model(self, image_bytes, prompt):
        image_url = "data:image/jpeg;base64," + base64.b64encode(image_bytes).decode("ascii")
        body = {
            "model": self.model,
            "temperature": 0,
            "response_format": TAGS_SCHEMA,
            "messages": [{
                "role": "user",
                "content": [
                    {"type": "text", "text": prompt},
                    {"type": "image_url", "image_url": {"url": image_url}},
                ],
            }],
        }
        reply = http_json("POST", self.api_url + "/chat/completions", body, timeout=self.args.timeout)
        return parse_model_answer(reply["choices"][0]["message"].get("content"))

    def process(self, item, prompt):
        image = http_request("GET", self.server + item["thumbnail_url"])
        model_tags = self.ask_model(image, prompt)
        tags = select_tags(model_tags, self.keywords, self.args.keywords_only, self.args.max_tags, self.args.marker)

        if self.args.dry_run:
            log.info("[dry-run] %s (id %d): %s", item["movie_filename"], item["id"], ", ".join(tags) or "-")
            return

        resp = http_json("POST", f"{self.server}/api/thumbnails/{item['id']}/tags", {"tags": tags + [self.args.marker]})
        log.info("%s (id %d): %s", item["movie_filename"], item["id"], ", ".join(tags) or "-")
        log.debug("Server tags now: %s", resp.get("tags"))

    def run(self):
        self.resolve_model()
        prompt = build_prompt(self.keywords, self.existing_tags(), self.args.keywords_only, self.args.max_tags)
        log.debug("Prompt:\n%s", prompt)

        done = failed = 0
        for item in self.pages():
            if self.stop or (self.args.max_images and done + failed >= self.args.max_images):
                break
            try:
                self.process(item, prompt)
                done += 1
            except (urllib.error.URLError, OSError, ValueError, KeyError, IndexError) as e:
                # No marker is written, so this image is retried on the next run
                failed += 1
                log.warning("Skipping %s (id %d): %s", item.get("movie_filename"), item.get("id"), e)
            if self.args.sleep:
                time.sleep(self.args.sleep)

        log.info("Finished: %d tagged, %d failed%s", done, failed, " (interrupted)" if self.stop else "")
        return 0 if failed == 0 else 1


def parse_args(argv):
    p = argparse.ArgumentParser(description="Tag movie thumbnails with a local vision model.")
    p.add_argument("--server", default="http://localhost:8080", help="thumbnailer base URL")
    p.add_argument("--api-url", default="http://localhost:1234/v1",
                   help="OpenAI-compatible API base (LM Studio default; Ollama: http://localhost:11434/v1)")
    p.add_argument("--model", help="model id (default: first model reported by the API)")
    p.add_argument("--keywords", help="comma-separated preferred tags")
    p.add_argument("--keywords-file", help="file with one preferred tag per line (# comments allowed)")
    p.add_argument("--keywords-only", action="store_true", help="only write tags from the keywords")
    p.add_argument("--no-existing-tags", action="store_true", help="don't offer the server's existing tags to the model")
    p.add_argument("--unviewed-only", action="store_true", help="only unviewed thumbnails")
    p.add_argument("--untagged-only", action="store_true", help="only thumbnails without any tag")
    p.add_argument("--marker", default="ai-reviewed", help="tag marking a thumbnail as analyzed (default: ai-reviewed)")
    p.add_argument("--max-tags", type=int, default=8, help="maximum tags per thumbnail (default: 8)")
    p.add_argument("--max-images", type=int, default=0, help="stop after N thumbnails (default: all)")
    p.add_argument("--timeout", type=float, default=300, help="model request timeout in seconds (default: 300)")
    p.add_argument("--sleep", type=float, default=0, help="pause between thumbnails in seconds")
    p.add_argument("--dry-run", action="store_true", help="analyze and print tags without writing them")
    p.add_argument("--verbose", action="store_true", help="debug logging")
    args = p.parse_args(argv)

    marker = clean_tag(args.marker)
    if not marker:
        p.error(f"invalid --marker {args.marker!r}")
    args.marker = marker
    if args.keywords_only and not (args.keywords or args.keywords_file):
        p.error("--keywords-only requires --keywords or --keywords-file")
    if args.max_tags < 1:
        p.error("--max-tags must be at least 1")
    return args


def main(argv=None):
    args = parse_args(argv)
    logging.basicConfig(level=logging.DEBUG if args.verbose else logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s")

    keywords = load_keywords(args.keywords, args.keywords_file)
    if keywords:
        log.info("Preferred keywords: %s", ", ".join(keywords))

    tagger = Tagger(args, keywords)

    def request_stop(signum, frame):
        if tagger.stop:
            sys.exit(130)
        log.info("Stopping after the current thumbnail (Ctrl-C again to abort)")
        tagger.stop = True

    signal.signal(signal.SIGINT, request_stop)
    return tagger.run()


if __name__ == "__main__":
    sys.exit(main())
