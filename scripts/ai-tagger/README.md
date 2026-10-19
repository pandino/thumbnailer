# AI tagger

Tags thumbnails with a local vision model. For each thumbnail that hasn't been analyzed yet, the script:

1. downloads the image (a contact sheet of frames from one movie),
2. asks the model for tags, preferring your keywords and the tags already in use,
3. adds those tags plus a marker tag (`ai-reviewed`) through the thumbnailer API.

The marker is what makes reruns cheap: the next run only fetches thumbnails *without* it. Images the model couldn't handle (bad reply, timeout) get no marker, so they're retried next time. Existing tags are never removed, and viewed status is never touched.

Requirements: Python 3.8+ (standard library only), a running thumbnailer, and an OpenAI-compatible server with a vision model loaded.

## Model server

Any OpenAI-compatible `/v1/chat/completions` endpoint that accepts images works.

- **LM Studio** (default `--api-url http://localhost:1234/v1`): load a vision-capable model (Gemma 3/4, Qwen2.5-VL, …) and start the server (`lms server start`).
- **Ollama**: `ollama pull qwen2.5vl`, then pass `--api-url http://localhost:11434/v1 --model qwen2.5vl`.

Without `--model`, the first model reported by `/v1/models` is used.

## Usage

```bash
# Try it on a few images without writing anything
./ai_tagger.py --server http://thumbnailer.genma.ch --dry-run --max-images 3 --verbose

# Tag everything not analyzed yet, preferring your keywords
./ai_tagger.py --server http://thumbnailer.genma.ch --keywords "beach,night,interview"

# Keywords from a file (one per line, # comments allowed); only ever write those
./ai_tagger.py --keywords-file keywords.txt --keywords-only

# Only unviewed images, or only images with no tags at all
./ai_tagger.py --unviewed-only
./ai_tagger.py --untagged-only
```

| Flag | Meaning |
|---|---|
| `--server` | Thumbnailer base URL (default `http://localhost:8080`) |
| `--api-url`, `--model` | Model server and model id |
| `--keywords`, `--keywords-file` | Preferred tags, listed to the model before the existing ones |
| `--keywords-only` | Drop any tag that isn't one of your keywords |
| `--no-existing-tags` | Don't offer the server's current tags to the model |
| `--unviewed-only`, `--untagged-only` | Restrict which thumbnails are processed |
| `--marker` | Marker tag (default `ai-reviewed`) |
| `--max-tags` | Tags per image (default 8) |
| `--max-images`, `--sleep`, `--timeout` | Pace and limit the run |
| `--dry-run` | Print tags, write nothing |

Ctrl-C stops after the current image; press it again to abort immediately. The exit code is 1 if any image failed.

## Re-analyzing

- Everything again, e.g. after changing model or keywords: use a new marker, `--marker ai-reviewed-v2`.
- A single image: remove its marker, `curl -X DELETE http://localhost:8080/api/thumbnails/<id>/tags/ai-reviewed`.

## Tests

```bash
python3 -m unittest discover -s scripts/ai-tagger
```
