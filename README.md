# Movie Thumbnailer

A Go application for generating and managing thumbnail mosaics from movie files. This project provides a modern, containerized Go application that includes a web interface for viewing and managing thumbnails.

## Features

- **Thumbnail Generation**: Creates thumbnail mosaics from movie files using FFmpeg
- **Background Scanning**: Runs scheduled scans for new movie files
- **Database Management**: Maintains SQLite database to track relationships and metadata
- **Web Interface**: Modern web interface for browsing and managing thumbnails
- **Slideshow Mode**: Interactive slideshow with keyboard controls for viewing/managing thumbnails
- **Session Tracking**: Tracks slideshow progress and provides undo functionality
- **Import Support**: Import existing thumbnails without regenerating them
- **Metadata Extraction**: Tracks movie duration, resolution, file size, and creation dates
- **Containerized**: Runs as a containerized application with minimal dependencies
- **Metrics Support**: Built-in Prometheus metrics for monitoring
- **Deletion Management**: Safe deletion with undo capabilities and background processing
- **Archive Management**: Archive movies to separate storage with background processing

## Prerequisites

- **Container Runtime**: Podman or Docker
- **For Development**: Go 1.24+ and optionally Task runner
- **System Requirements**: FFmpeg is included in the container image

## Quick Start

### Using Podman/Docker

1. Build the container image:
   ```bash
   podman build -t movie-thumbnailer-go .
   ```

   Or using Task:
   ```bash
   task build
   ```

2. Run the container:
   ```bash
   podman run -d --name movie-thumbnailer \
     -v "$(pwd)/movies:/movies:ro" \
     -v "$(pwd)/thumbnails:/thumbnails" \
     -v "$(pwd)/data:/data" \
     -p 8080:8080 \
     movie-thumbnailer-go
   ```

3. Access the web interface at http://localhost:8080

### Importing Existing Thumbnails

If you already have thumbnail files generated and want to import them without regenerating, use the `--import-existing` flag:

```bash
podman run -d --name movie-thumbnailer \
  -v "$(pwd)/movies:/movies:ro" \
  -v "$(pwd)/thumbnails:/thumbnails" \
  -v "$(pwd)/data:/data" \
  -p 8080:8080 \
  movie-thumbnailer-go --import-existing
```

You can also set the environment variable `IMPORT_EXISTING=true` to enable this feature.

When using this feature:
- Existing thumbnails will be marked with a special "imported" source tag
- Metadata (duration, resolution, etc.) will be extracted from the original movie file
- The thumbnail file won't be regenerated, saving processing time
- Imported thumbnails can be browsed, viewed, and managed just like generated ones

## Configuration

You can configure the application by setting environment variables:

### Directory Settings
- `MOVIE_INPUT_DIR`: Directory containing movie files (default: `/movies`)
- `THUMBNAIL_OUTPUT_DIR`: Directory for generated thumbnails (default: `/thumbnails`)
- `DATA_DIR`: Directory for data storage (default: `/data`)
- `ARCHIVE_DIR`: Directory for archived movies (default: `/archive`)

### Thumbnail Generation
- `GRID_COLS`: Number of columns in the thumbnail grid (default: `8`)
- `GRID_ROWS`: Number of rows in the thumbnail grid (default: `4`)
- `MAX_WORKERS`: Maximum number of concurrent thumbnail generation processes (default: `4`)
- `FILE_EXTENSIONS`: Comma-separated list of movie file extensions to scan (default: `mp4,mkv,avi,mov,mts,wmv`)

### Server Settings
- `SERVER_PORT`: Port for the web server (default: `8080`)
- `SERVER_HOST`: Host for the web server (default: `0.0.0.0`)

### Background Task Settings
- `SCAN_INTERVAL`: Interval between background scans (default: `1h`)
- `DEBUG`: Enable debug logging (default: `false`)
- `DISABLE_DELETION`: Disable deletion worker and prevent processing of deletion queue (default: `false`)
- `IMPORT_EXISTING`: Import existing thumbnails without regenerating (default: `false`)

### Monitoring Settings
- `METRICS_PORT`: Port for Prometheus metrics endpoint (default: same as `SERVER_PORT`)
- The application exposes metrics at `/metrics` endpoint for Prometheus monitoring

## Database Schema

Thumbnails are stored in a SQLite database with the following schema:

```sql
CREATE TABLE thumbnails (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_path TEXT NOT NULL UNIQUE,
    movie_filename TEXT NOT NULL,
    thumbnail_path TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    status TEXT DEFAULT 'pending',
    viewed INTEGER DEFAULT 0,
    width INTEGER DEFAULT 0,
    height INTEGER DEFAULT 0,
    duration REAL DEFAULT 0,
    file_size INTEGER DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    source TEXT DEFAULT 'generated'
);
```

Key fields:
- `status`: Current processing status ('pending', 'success', 'error', 'deleted', 'archived')
- `viewed`: Whether the thumbnail has been viewed by the user (0 or 1)
- `source`: How the thumbnail was created ('generated' or 'imported')
- `file_size`: Size of the movie file in bytes

## Web Interface

The application provides two main pages:

### Control Page (/)
- **Dashboard**: Displays comprehensive statistics about movies and thumbnails
- **Session Management**: Shows current slideshow session progress if active
- **Manual Controls**: Buttons for scanning, cleanup, processing deletions, and processing archival
- **Thumbnail Lists**: Browse thumbnails by status (unviewed, viewed, deleted, archived, errors)
- **Real-time Updates**: Dynamic loading of thumbnail data via JavaScript
- **Slideshow Launcher**: Start new slideshow sessions from unviewed thumbnails

### Slideshow Page (/slideshow)
- Displays thumbnails in a fullscreen slideshow view
- Shows random unviewed thumbnails
- Tracks session progress and statistics
- Keyboard shortcuts:
  - **→** (Right arrow) or **Space**: Mark as viewed and go to next thumbnail
  - **U**: Undo last action (single-level undo)
  - **D**: Delete current thumbnail/movie
  - **M**: Archive current thumbnail/movie
  - **S**: Skip to next thumbnail without marking as viewed
  - **Esc**: Return to control page

### Additional Features

- **Task Runner**: Includes `Taskfile.yml` for common development tasks
- **Docker Support**: Complete containerization with multi-stage builds
- **Local Development**: Easy local development setup with test data
- **Error Handling**: Comprehensive error handling and logging
- **Migration Support**: Database migration capabilities
- **Cleanup Tools**: Automatic orphan cleanup and deletion queue processing

### API Endpoints

The application provides several API endpoints for programmatic access:

- `GET /api/stats` - Get application statistics
- `GET /api/thumbnails` - List thumbnails (supports filtering by status, viewed state)
- `GET /api/thumbnails/{id}` - Get specific thumbnail details
- `GET /api/slideshow/next-image` - Preload next slideshow image
- `POST /api/v1/video/archive` - Archive a video by filename
- `POST /api/v1/video/delete` - Delete a video by filename
- `GET /api/v1/video/status/{filename}` - Get video status by filename

For detailed monitoring capabilities, see `METRICS.md` for comprehensive Prometheus metrics documentation.

## Development

### Building from Source

1. Install Go (version 1.24 or higher)
2. Clone the repository
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Build the application:
   ```bash
   go build -o movie-thumbnailer ./cmd/movie-thumbnailer
   ```

Alternative build using Task:
```bash
# Install Task (if not already installed)
# Then build using the provided Taskfile
task gobuild
```

### Local Development Setup

For local development and testing:

```bash
# Build the application
task gobuild

# Run locally with test data
./runlocal.sh
```

The `runlocal.sh` script sets up the application with test data and enables debug mode.

### Running Tests

```bash
go test ./...
```

Or using Task:
```bash
task test
```

### Available Task Commands

The project includes a `Taskfile.yml` with several useful commands:

- `task build` - Build container image
- `task gobuild` - Build Go executable locally  
- `task test` - Run tests in container
- `task clean` - Clean build artifacts
- `task version` - Display version information

## Architecture

The application follows a clean architecture pattern:

- **`cmd/`** - Application entry points
- **`internal/config`** - Configuration management
- **`internal/database`** - Database operations and models
- **`internal/ffmpeg`** - FFmpeg thumbnail generation
- **`internal/scanner`** - File system scanning and background tasks
- **`internal/server`** - HTTP server and request handlers
- **`internal/worker`** - Background job processing
- **`internal/metrics`** - Prometheus metrics collection
- **`web/`** - Static assets and templates

## Troubleshooting

### Common Issues

**Container won't start:**
- Ensure the mounted directories exist and are writable
- Check that port 8080 is not already in use

**No thumbnails being generated:**
- Verify FFmpeg is working by checking container logs
- Ensure movie files are in supported formats (mp4, mkv, avi, mov, mts, wmv)
- Check file permissions on the movies directory

**Performance issues:**
- Adjust `MAX_WORKERS` environment variable based on CPU cores
- Monitor metrics at `/metrics` endpoint to identify bottlenecks
- Consider adjusting `GRID_COLS` and `GRID_ROWS` for smaller thumbnails

**High memory usage:**
- Reduce `MAX_WORKERS` for concurrent processing
- Check for large movie files that may require more memory for processing

## License

This project is open source. See the repository for license details.

## Contributing

Contributions are welcome! Please feel free to submit issues, feature requests, or pull requests.

## Acknowledgments

- Uses FFmpeg for robust thumbnail generation
- Built with Go for performance and reliability  
- Containerized for easy deployment and portability