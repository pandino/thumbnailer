# Movie Thumbnailer

A Go application for generating and managing thumbnail mosaics from movie files. This project replaces the original shell script-based solution with a modern, containerized Go application that includes a web interface for viewing and managing thumbnails.

## Features

- Generates thumbnail mosaics from movie files using FFmpeg
- Runs scheduled background scans for new movie files
- Maintains a SQLite database to track relationships
- Provides a web interface for browsing and managing thumbnails
- Runs as a containerized application with minimal dependencies

## Prerequisites

- Docker
- Docker Compose (optional, but recommended)

## Quick Start

### Using Docker Compose (Recommended)

1. Clone this repository:
   ```bash
   git clone https://github.com/pandino/movie-thumbnailer-go.git
   cd movie-thumbnailer-go
   ```

2. Create directories for movies, thumbnails, and data:
   ```bash
   mkdir -p movies thumbnails data
   ```

3. Place your movie files in the `movies` directory.

4. Build and run the container:
   ```bash
   # Build the container
   docker-compose build

   # Run the container
   docker-compose up -d
   ```

5. Access the web interface at http://localhost:8080

### Using Docker Directly

1. Build the Docker image:
   ```bash
   docker build -t movie-thumbnailer-go .
   ```

2. Run the container:
   ```bash
   docker run -d --name movie-thumbnailer \
     -v "$(pwd)/movies:/movies:ro" \
     -v "$(pwd)/thumbnails:/thumbnails" \
     -v "$(pwd)/data:/data" \
     -p 8080:8080 \
     movie-thumbnailer-go
   ```

3. Access the web interface at http://localhost:8080

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
