# Preference model worker

Learns which movies you delete and which you keep, from the frames of their thumbnail grids, and scores the movies you haven't decided on yet. The slideshow shows each score as a badge and can sort by it.

The thumbnailer server has no GPU and a weak CPU, so this worker runs on a desktop that's only on some of the time. The server stores everything the worker produces (embeddings and scores), so the slideshow uses the latest scores even while the desktop is off.

## What it learns from

The server logs every decision (`/api/v1/ml/decisions`):

| Where | Decision | Delete model ("genre") | Watch model |
|---|---|---|---|
| Slideshow | `D` delete | 1 | – |
| Slideshow | Space/→ (viewed, "pass") or archive | 0 | – |
| Slideshow | `S` skip | no label | – |
| Player (MPV scripts) | archive | 0 | 1 |
| Player (MPV scripts) | delete | 0 | 0 |

`U` (undo) cancels the decision it undoes. The grids of deleted and archived movies are kept in `thumbnails/decided/`, so the training history survives the files themselves.

The **delete model** predicts the slideshow decision from the grid alone. The **watch model** predicts what you'll do after watching. It's only trained once it has at least 20 examples of each outcome, and until then nothing uses it.

## How a run works

1. **Embed.** Each grid without an embedding is downloaded and cut into its tiles (the server reports the layout). Blank tiles are dropped and up to 16 evenly spaced frames are embedded with SigLIP 2 (`ViT-B-16-SigLIP2-256`). The frame embeddings are averaged. This takes about 1.3 s per grid on a Ryzen 7 5700X3D CPU (0.15 s on the GPU, see below), and each run stops after `--max-minutes` (default 20), so the first pass over a few thousand grids is spread over a few runs.
2. **Train.** A class-balanced logistic regression per model, with cross-validated regularization. Its cross-validated AUC appears on the control page.
3. **Score.** All live grids are scored and uploaded, replacing the previous scores.

## Install

```bash
./install.sh   # ROCm venv in ~/.local/share/thumbnailer-taste, systemd user timer (boot + every 15 min)
```

The timer embeds on the GPU (see [GPU](#gpu-amd-rocm)); `install.sh` refuses to install it if torch can't use the GPU.

The service points at `http://192.168.1.35:8080`, the container's LAN address. The public hostname is behind SSO. Edit `systemd/thumbnailer-taste.service` if that changes, then re-run `install.sh`.

## Run by hand

```bash
export HSA_OVERRIDE_GFX_VERSION=10.3.0
PY=~/.local/share/thumbnailer-taste/venv-rocm/bin/python
$PY taste.py --server http://192.168.1.35:8080 --device cuda   # embed (20 min max), train, score
$PY taste.py --server http://192.168.1.35:8080 --no-embed      # retrain and rescore only
$PY taste.py --server http://192.168.1.35:8080 --dry-run -v    # don't upload scores
journalctl --user -u thumbnailer-taste -f                      # timer runs
```

| Flag | Meaning |
|---|---|
| `--arch`, `--pretrained` | open_clip model (default `ViT-B-16-SigLIP2-256` / `webli`) |
| `--max-frames` | frames per grid, 0 for all 32 (default 16) |
| `--max-minutes` | time box for embedding (default 20) |
| `--device` | `cpu` (default), `cuda` (GPU) or `auto` (GPU if usable) |
| `--threads` | torch CPU threads |
| `--no-embed`, `--dry-run` | skip embedding / skip uploading scores |

### GPU (AMD, ROCm)

The RX 6700 XT embeds a grid in about 0.15 s, against 1.3 s on the CPU. It needs a ROCm build of torch, which `install.sh` puts in its own venv (about 15 GB, as it bundles ROCm), and an override, because gfx1031 isn't an official ROCm target: `HSA_OVERRIDE_GFX_VERSION=10.3.0`, set in the service. Without it torch crashes on the first GPU call. Ignore the `(null): No such file or directory` line ROCm prints.

GPU and CPU embeddings were checked to be identical (cosine 1.00000), so `--device cpu` (the script's default, with a CPU-only torch) fills the same embedding set if the GPU is ever unusable.

VRAM: the base model needs about 1.6 GiB, on top of whatever the llama router has loaded. The router's presets use up to about 11 GiB of the 12, so with the biggest ones loaded a run can fail for lack of memory, or push part of the router's model out of VRAM until the run ends; the next timer run simply retries. `ViT-SO400M-16-SigLIP2-256` needs 4.6 GiB, so stop the router before trying it.

Embeddings are stored under a key made of model, weights and frame count (e.g. `ViT-B-16-SigLIP2-256/webli/f16`). Changing any of them starts a fresh embedding pass, and old embeddings stay on the server unused.

## Tests

```bash
python3 -m unittest discover -s scripts/taste
```
