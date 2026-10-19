# AI tagger

Tags thumbnails with a local vision model, by default Gemma 4 vision on the llama.cpp router. For each thumbnail that hasn't been analyzed yet, the script:

1. downloads the image (a contact sheet of frames from one movie),
2. asks the model for tags, preferring your keywords and the tags already in use,
3. adds those tags plus a marker tag (`ai-reviewed`) through the thumbnailer API.

The marker is what makes reruns cheap: the next run only fetches thumbnails *without* it. Images the model couldn't handle (bad reply, timeout) get no marker, so they're retried next time. Existing tags are never removed, and viewed status is never touched.

Requirements: Python 3.8+ (standard library only), a running thumbnailer (v0.7.0+, for `/api/v1/thumbnails`), and an OpenAI-compatible server with a vision model.

## Model server

The defaults target the **llama.cpp router** (`llama-daemon`, `http://localhost:8080/v1`) with the `gemma-4-12b-vision` preset: Gemma 4 12B plus its vision projector, `reasoning = off`. That runs at about **3.7 s per image**, so 10,000 thumbnails take roughly 10 hours.

```bash
llama-daemon status          # router up, which model is loaded
llama-daemon start           # if it isn't running
```

> **Run one model server at a time.** The router and LM Studio share the 12 GB GPU. Loading Gemma in LM Studio while the router holds it pushes VRAM past the limit, and everything slows to a crawl: a measured ~146 s per image instead of ~3.7 s. LM Studio also runs Gemma with thinking on, which costs minutes per image on its own.

Any other OpenAI-compatible `/v1/chat/completions` endpoint that accepts images works too:

- **LM Studio**: `--api-url http://localhost:1234/v1 --model <id>`. Stop the router first, and turn thinking off for the model. Always pass `--model`, because `/v1/models` lists every downloaded model, not just the loaded one.
- **Ollama**: `--api-url http://localhost:11434/v1 --model qwen2.5vl`.

`--model ""` picks the first model reported by `/v1/models`.

## Usage

```bash
# Try it on a few images without writing anything
./ai_tagger.py --server http://thumbnailer.genma.ch --dry-run --max-images 3 --verbose

# Tag everything not analyzed yet, preferring your keywords
./ai_tagger.py --server http://thumbnailer.genma.ch --keywords "beach,night,interview"

# Keywords from a file (one per line, # comments allowed); only ever write those
./ai_tagger.py --server http://thumbnailer.genma.ch --keywords-file keywords.txt --keywords-only

# Only unviewed images, or only images with no tags at all
./ai_tagger.py --server http://thumbnailer.genma.ch --unviewed-only
./ai_tagger.py --server http://thumbnailer.genma.ch --untagged-only
```

| Flag | Meaning |
|---|---|
| `--server` | Thumbnailer base URL (**required**; there's no default because the router also uses port 8080) |
| `--api-url`, `--model` | Model server and model id (default: router at `http://localhost:8080/v1`, `gemma-4-12b-vision`) |
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
- A single image: remove its marker, `curl -X DELETE http://thumbnailer.genma.ch/api/thumbnails/<id>/tags/ai-reviewed`.

## Tests

```bash
python3 -m unittest discover -s scripts/ai-tagger
```
