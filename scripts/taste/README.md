# Preference model worker

Learns which movies you end up deleting and which you end up archiving, from the frames of their thumbnail grids, and scores the movies you haven't decided on yet. The slideshow shows each score as a badge and can sort by it.

The thumbnailer server has no GPU and a weak CPU, so this worker runs on a desktop that's only on some of the time. The server stores everything the worker produces (embeddings and scores), so the slideshow uses the latest scores even while the desktop is off.

## What it learns from

The server logs every decision (`/api/v1/ml/decisions`). Every movie ends up in one of two places, so the model learns one ordered outcome, wherever the decision was made:

| Outcome | From |
|---|---|
| **deleted** | `D` in the slideshow, or delete in MPV after watching |
| **undecided** | Space/→ in the slideshow ("pass": watch it before deciding), not decided in MPV yet |
| **archived** | archive (`M`) in the slideshow, or archive in MPV after watching |
| no outcome | `S` (skip), or a decision undone with `U` |

A passed movie moves from undecided to deleted or archived once you decide on it in MPV; if a movie has both a delete and an archive, the later one counts. A delete made after watching counts half as much in the delete model as a delete from the slideshow (`--watched-delete-weight`, default 0.5): the grid looked worth watching, so it says less about what to delete on sight. Archives count fully wherever they're made. The grids of deleted and archived movies are kept in `thumbnails/decided/`, so the training history survives the files themselves.

The ordering deleted < undecided < archived is modelled with two classifiers over all decided movies (after Frank & Hall): one for P(deleted), one for P(archived); the rest is "still undecided". Measured on production data, this ranks better than a single delete-vs-rest model (AUC deleted vs archived 0.93 vs 0.91, archived among undecided 0.82 vs 0.74) and as well at spotting deletes among undecided movies (0.78). Each model needs at least 10 movies of its outcome before it's trained.

## How a run works

1. **Embed.** Each grid without an embedding is downloaded and cut into its tiles (the server reports the layout). Blank tiles are dropped and up to 16 evenly spaced frames are embedded with SigLIP 2 (`ViT-B-16-SigLIP2-256`). The frame embeddings are averaged. This takes about 1.3 s per grid on a Ryzen 7 5700X3D CPU (0.15 s on the GPU, see below), and each run stops after `--max-minutes` (default 20), so the first pass over a few thousand grids is spread over a few runs.
2. **Train.** A class-balanced logistic regression for each outcome (deleted, archived), with cross-validated regularization, so the rarer outcome isn't drowned out. Their cross-validated AUCs appear on the control page (`auc_delete`, `auc_archive`).
3. **Calibrate.** Balanced weighting ranks well but inflates the probabilities: on production data, movies scored "50–70%" were actually deleted 15% of the time. A Platt scaling fitted on each model's out-of-fold scores maps them to observed frequencies. It is monotonic, so the ranking and the slideshow orders don't change; the Brier scores (lower is better) are reported as `brier_delete` and `brier_archive`. If the two probabilities add up to more than 1 they are scaled down together.
4. **Score.** All live grids are scored (`p_delete`, `p_archive`) and uploaded, replacing the previous scores. The slideshow badge shows the likelier outcome ("Delete 30%" / "Archive 40%"); "likely delete first" sorts by `p_delete`, "likely archive first" by `p_archive`, and "most uncertain first" puts first the movies whose likeliest outcome is least likely.

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
| `--watched-delete-weight` | weight of a delete made after watching, relative to a slideshow delete (default 0.5) |
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
