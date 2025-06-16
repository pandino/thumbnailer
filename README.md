# Movie Thumbnailer

A Go application for generating and managing thumbnail mosaics from movie files. This project provides a modern, containerized Go application that includes a web interface for viewing and managing thumbnails.

## Features

- Generates thumbnail mosaics from movie files using FFmpeg
- Runs scheduled background scans for new movie files
- Maintains a SQLite database to track relationships
- Provides a web interface for browsing and managing thumbnails
- Tracks movie metadata including duration, resolution, and file size
- Runs as a containerized application with minimal dependencies
- Supports importing existing thumbnails without regenerating them

## Prerequisites

- Podman

## Quick Start

### Using Podman

1. Build the container image:
   ```bash
   podman build -t movie-thumbnailer-go .
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

### Preventing File Deletion

To prevent the actual deletion of files marked for deletion, use the `--prevent-deletion` flag:

```bash
podman run -d --name movie-thumbnailer \
  -v "$(pwd)/movies:/movies:ro" \
  -v "$(pwd)/thumbnails:/thumbnails" \
  -v "$(pwd)/data:/data" \
  -p 8080:8080 \
  movie-thumbnailer-go --prevent-deletion
```

This can also be set via the environment variable `PREVENT_DELETION=true`.

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
- `IMPORT_EXISTING`: Import existing thumbnails without regenerating (default: `false`)
- `PREVENT_DELETION`: Prevent deletion of images marked for deletion (default: `false`)

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
- `status`: Current processing status ('pending', 'success', 'error', 'deleted')
- `viewed`: Whether the thumbnail has been viewed by the user (0 or 1)
- `source`: How the thumbnail was created ('generated' or 'imported')
- `file_size`: Size of the movie file in bytes

## Web Interface

The application provides two main pages:

### Control Page (/)
- Displays statistics about movies and thumbnails
- Provides controls for manual scanning and cleanup
- Shows lists of thumbnails by status

### Slideshow Page (/slideshow)
- Displays thumbnails in a fullscreen view
- Keyboard shortcuts:
  - Right arrow or Space: Next thumbnail
  - Left arrow: Previous thumbnail
  - 'M': Mark current thumbnail as viewed
  - 'D': Delete current thumbnail/movie
  - 'R': Reset history
  - 'ESC': Return to control page

## Development

### Building from Source

1. Install Go (version 1.21 or higher)
2. Clone the repository
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Build the application:
   ```bash
   go build -o movie-thumbnailer ./cmd/movie-thumbnailer
   ```

### Running Tests

```bash
go test ./...
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Based on the original shell script implementation
- Uses FFmpeg for thumbnail generation