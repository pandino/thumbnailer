# Docker Movie Thumbnailer

This project provides a containerized solution for generating thumbnail mosaics for movie files. It uses FFmpeg's tile filter to create a 2×6 grid of frames extracted from each movie.

## Features

- Lightweight Alpine-based container (~150MB)
- Generates thumbnail mosaics from movie files
- Maintains a SQLite database to track relationships
- Provides synchronization and cleanup features
- Runs as a non-root user for better security

## Prerequisites

- Docker
- Docker Compose (optional, but recommended)

## Quick Start

### Using Docker Compose (Recommended)

1. Create the following directory structure:
   ```
   movie-thumbnailer/
   ├── Dockerfile
   ├── docker-compose.yml
   ├── movie-thumbnailer.sh
   ├── movies/            # Place your movie files here
   ├── thumbnails/        # Generated thumbnails will be stored here
   └── data/              # SQLite database will be stored here
   ```

2. Place the movie-thumbnailer script, Dockerfile, and docker-compose.yml in the root directory.

3. Build and run the container:
   ```bash
   # Build the container
   docker-compose build

   # Scan for movies and generate thumbnails
   docker-compose run movie-thumbnailer scan

   # Synchronize database with files
   docker-compose run movie-thumbnailer sync

   # Show help
   docker-compose run movie-thumbnailer --help
   ```

### Using Docker Directly

1. Build the Docker image:
   ```bash
   docker build -t movie-thumbnailer .
   ```

2. Run the container:
   ```bash
   # Show help
   docker run --rm movie-thumbnailer

   # Scan for movies and generate thumbnails
   docker run --rm \
     -v "$(pwd)/movies:/movies:ro" \
     -v "$(pwd)/thumbnails:/thumbnails" \
     -v "$(pwd)/data:/data" \
     movie-thumbnailer scan

   # Synchronize database with files
   docker run --rm \
     -v "$(pwd)/movies:/movies:ro" \
     -v "$(pwd)/thumbnails:/thumbnails" \
     -v "$(pwd)/data:/data" \
     movie-thumbnailer sync
   ```

## Configuration

You can configure the container by passing environment variables:

```bash
docker run --rm \
  -v "$(pwd)/movies:/movies:ro" \
  -v "$(pwd)/thumbnails:/thumbnails" \
  -v "$(pwd)/data:/data" \
  -e MAX_WORKERS=8 \
  -e INPUT_DIR=/movies \
  -e OUTPUT_DIR=/thumbnails \
  -e DB_FILE=/data/thumbnailer.db \
  movie-thumbnailer scan
```

Available environment variables:
- `INPUT_DIR`: Directory containing movie files (default: `/movies`)
- `OUTPUT_DIR`: Directory for generated thumbnails (default: `/thumbnails`)
- `DB_FILE`: Path to SQLite database file (default: `/data/thumbnailer.db`)
- `MAX_WORKERS`: Maximum number of concurrent processes (default: `4`)

The script is fully configurable through these environment variables, which take precedence over command-line arguments.

## Advanced Usage

### Using Different Directories

```bash
docker run --rm \
  -v "/path/to/my/movies:/input:ro" \
  -v "/path/to/my/thumbnails:/output" \
  -v "/path/to/my/data:/data" \
  -e INPUT_DIR=/input \
  -e OUTPUT_DIR=/output \
  movie-thumbnailer scan
```

### Deleting Orphaned Movies

To delete movie files that don't have thumbnails during sync:

```bash
docker run --rm \
  -v "$(pwd)/movies:/movies" \
  -v "$(pwd)/thumbnails:/thumbnails" \
  -v "$(pwd)/data:/data" \
  movie-thumbnailer sync --delete-orphans
```

**Note**: For this operation, you must mount the movies directory with write permissions.

## Troubleshooting

### Permission Issues

If you encounter permission issues, ensure that the directories you're mounting have appropriate permissions:

```bash
chmod -R 755 movies thumbnails data
```

### Database Corruption

If the SQLite database becomes corrupted, you can delete it and restart:

```bash
rm data/thumbnailer.db
docker-compose run movie-thumbnailer scan
```

## Security Considerations

The container runs as a non-root user (`thumbnailer`) for improved security. The movie directory is mounted as read-only by default to prevent any accidental modifications to your original files.