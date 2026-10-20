# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This repo is one of the btnet homelab sub-projects. See `../CLAUDE.md` for how it is deployed to ichinose (Quadlet container, image `docker.io/pandino/thumbnailer`, version pinned in `btnet/host_vars/ichinose/applications.yaml`).

## Commands

```bash
task gobuild           # Build the Go binary to build/movie-thumbnailer with version ldflags
./runlocal.sh          # Build output + run against ./test_data with DEBUG and DISABLE_DELETION on
go test ./...          # Run all tests (or `task test` to run them in a golang:1.24-alpine container)
go test ./internal/server -run TestName   # Run a single test
task build             # Build the container image (podman) with embedded version/commit/build-date
task publish           # Push to Docker Hub — clean tagged commit: :version+:latest; untagged on main: :latest only; other branches: :debug
```

Versions come from git tags (`git describe --tags --always --dirty`, e.g. `v0.6.0-2-gabc1234`); there is no VERSION file. New features bump the minor version (`vX.Y.0`), fixes the patch. Note `task publish`'s dirty check is `git diff --quiet`, so staged or untracked changes don't block a versioned push.

There is no separate lint step; rely on `go vet ./...` and `gofmt`.

## Architecture

A single Go binary (`cmd/movie-thumbnailer`) that scans a movies directory, generates JPEG thumbnail-grid mosaics with ffmpeg, tracks everything in SQLite, and serves a web UI + REST API. A second binary (`cmd/migrate`) runs schema/data migrations and is invoked by `docker-entrypoint.sh` **before** the main app starts in the container.

Wiring happens in `cmd/movie-thumbnailer/main.go`: it builds config → database → scanner → server → worker, then runs the server and worker as goroutines. Note the scanner is constructed twice — once without metrics, then re-created with the server's metrics instance and pushed back via `srv.UpdateScanner`. The metrics object lives on the `Server` and is shared into scanner/worker/ffmpeg.

### Packages (`internal/`)
- **config** — all configuration is environment-variable driven (`config.New()`); see README for the full list. `DATABASE_PATH` overrides the default `${DATA_DIR}/thumbnailer.db`.
- **database** — SQLite via `mattn/go-sqlite3` (CGO required, hence `CGO_ENABLED=1` in the Dockerfile). **Pool is capped at 1 connection** (`SetMaxOpenConns(1)`) to avoid `database is locked` errors — all DB access is effectively serialized. Schema is created in `initSchema`; the data model is the `thumbnails` table plus the preference-model tables in `ml.go`. Tables keyed by thumbnail ID deliberately have no FK/cascade: `UpsertThumbnail` uses `INSERT OR REPLACE`, and the model's tables must outlive the thumbnail row anyway. Tags were removed in v0.9.0 (`initSchema` drops `thumbnail_tags`).
- **ffmpeg** — shells out to `ffmpeg`/`ffprobe` (bundled in the container image). `CreateThumbnail` probes metadata, computes a keyframe interval, and builds the grid (`GRID_COLS`×`GRID_ROWS`).
- **scanner** — the core engine. `ScanMovies` reads the **top level only** of `MoviesDir` (no recursion), processes new files in parallel via `errgroup` limited to `MaxWorkers`, then runs `CleanupOrphans`. A `sync.Mutex` + `isScanning` flag guarantees only one scan at a time. `CleanupOrphans` also drains the archival and deletion queues and removes DB rows / thumbnail files for movies that no longer exist on disk.
- **worker** — owns the background tickers: initial scan at startup, periodic scan every `SCAN_INTERVAL`, cleanup every 6h. Skips ticks while a scan is in progress. All of these only run inside the work window (`WORK_WINDOW_START`/`WORK_WINDOW_END`, default 11-20 local time); scans get a context deadline at window close, and a scan also starts when the window opens. HTTP-triggered scans/cleanups bypass the window. Also exposes on-demand `PerformScan`/`PerformCleanup` for the HTTP handlers.
- **server** — gorilla/mux router, logging + panic-recovery middleware, serves templates (`web/templates`), static assets (`web/static`), and the thumbnails dir. Routes split into control-page, slideshow, `/api`, and `/api/v1/video` groups, plus `/metrics`.
- **models** — the `Thumbnail` struct and `Stats`, plus the status/source string constants below.
- **metrics** — Prometheus collectors; see `METRICS.md`.

### Status / source lifecycle
A thumbnail row's `status` is one of `pending`, `success`, `error`, `deleted`, `archived` (constants in `internal/models/models.go`). `source` is `generated` or `imported`. **Deletion and archival are queue-based, not immediate**: the UI/API sets the row's status to `deleted`/`archived`, and the scanner's cleanup pass later actually removes or moves the underlying movie file. `DISABLE_DELETION=true` stops the deletion queue from being drained (used in local dev so test movies are never destroyed). Archival copies the file to `ARCHIVE_DIR` preserving its name, then removes the original. In both cases the grid is **moved** to `${THUMBNAIL_OUTPUT_DIR}/decided/<id>.jpg` and the movie snapshotted in `decided_movies` (`Scanner.keepDecidedGrid`) before the row is deleted: they are the preference model's training history.

### Preference model
Every decision is appended to the `decisions` table in the same transaction as the status change (`DB.updateWithDecision`): `MarkAsViewedByID` = slideshow `pass`, `MarkForDeletionByID`/`MarkForArchivalByID` = slideshow `delete`/`archive`, `MarkForDeletion`/`MarkForArchival(id, StagePlayer)` from the MPV endpoints, `RestoreFromDeletionByID` = `undo`. The slideshow's delayed commit means an in-session undo logs nothing. `decisions`, `decided_movies`, `grid_geometry`, `embeddings`, `scores` and `ml_meta` (all in `internal/database/ml.go`) are never touched by cleanup or reset-views. Grid tile layout constants live in `internal/ffmpeg` (`TileWidth` etc.) and are recorded per thumbnail in `grid_geometry`; `Scanner.BackfillGeometry` fills it in after every scan. The ML itself runs off-server in `scripts/taste/` (Python, desktop CPU): it pulls grids/decisions from `/api/v1/ml/*`, pushes embeddings and scores back. `BACKFILL_LABELS=true` (`Scanner.BackfillLabels`, after each scan, idempotent) reconstructs pre-logging history from `thumbnails/bootstrap/` (a db snapshot plus `deleted/<id>.jpg`), viewed rows, and the movies already in `ARCHIVE_DIR`.

### Slideshow sessions
Session state is **stateless on the server** — it lives entirely in a base64-encoded JSON `slideshow_session` cookie (`SessionData` in `internal/server/handlers.go`). There is no server-side session store. Single-level undo, per-session deleted-size tracking, the score order (`Order`, `&order=likely-delete|uncertain|likely-keep`) and the recently skipped IDs (`Skipped`) are part of this cookie payload. Every pick in the slideshow handlers goes through `s.randomUnviewed(session, …)` so the order and skips are honored; score orders are deterministic, so callers must exclude the current thumbnail.

### `/api/v1/video/*` and MPV
`POST /api/v1/video/archive` and `/delete` take a movie filename and enqueue the status change; `GET /api/v1/video/status/{filename}` reports current state. These exist so the MPV Lua scripts in `scripts/mpv/` can archive/delete the currently playing file and skip to the next. The scripts support a native-HTTP mode (no curl dependency) with auto-detection — see `scripts/mpv/README.md`.

`/api/v1/ml/*` (`internal/server/ml.go`) serves the preference model worker in `scripts/taste/` (tests: `python3 -m unittest discover -s scripts/taste`): `items` (live + decided grids with geometry, `missing_model=` filter), `embeddings` GET/PUT, `decisions`, `scores` PUT (replaces all) and `status`.

## Conventions
- Logging is `logrus` with structured fields (`WithField`/`WithFields`); follow the existing field naming (`movie`, `thumbnail`, `archive`).
- Long-running loops in scanner/worker check `ctx.Done()` periodically (every N iterations) for cooperative cancellation — preserve this when adding loops.
- `version`/`commit`/`buildDate` are injected via `-ldflags` at build time; `cmd/movie-thumbnailer/version.go` declares them.
