# Prevent Deletion Feature Implementation

## Overview
Added a flag to prevent the deletion of images and movies marked for deletion. This feature can be activated through both environment variables and command-line parameters.

## Implementation Details

### 1. Configuration Changes
- Added `PreventDeletion bool` field to the `Config` struct in `internal/config/config.go`
- Added support for `PREVENT_DELETION` environment variable (default: `false`)

### 2. Command Line Flag
- Added `--prevent-deletion` flag to the main application
- Flag description: "Prevent deletion of images marked for deletion"

### 3. Scanner Modification
- Modified `processDeletedItems()` function in `internal/scanner/scanner.go`
- When `PreventDeletion` is enabled, the function logs a message and returns early without processing deletions

### 4. Documentation Updates
- Updated README.md with both environment variable and command-line flag documentation
- Added usage examples for container deployment

## Usage Examples

### Command Line
```bash
./movie-thumbnailer --prevent-deletion
```

### Environment Variable
```bash
PREVENT_DELETION=true ./movie-thumbnailer
```

### Container (Podman/Docker)
```bash
podman run -d --name movie-thumbnailer \
  -v "$(pwd)/movies:/movies:ro" \
  -v "$(pwd)/thumbnails:/thumbnails" \
  -v "$(pwd)/data:/data" \
  -p 8080:8080 \
  -e PREVENT_DELETION=true \
  movie-thumbnailer-go
```

## Behavior

### When Enabled
- Items marked for deletion remain in the database with status `'deleted'`
- Physical files (movies and thumbnails) are NOT deleted from disk
- Log message: "Deletion prevention is enabled - skipping processing of items marked for deletion"
- Users can still undo deletions through the web interface

### When Disabled (Default)
- Normal deletion behavior: items marked for deletion are physically removed
- Database entries are cleaned up
- Files are deleted from disk during cleanup operations

## Testing
The feature has been tested with both environment variable and command-line flag approaches. The application correctly:
1. Parses the configuration
2. Logs the activation of deletion prevention
3. Skips deletion processing when enabled

## Files Modified
1. `internal/config/config.go` - Added configuration support
2. `cmd/movie-thumbnailer/main.go` - Added command-line flag
3. `internal/scanner/scanner.go` - Added deletion prevention logic
4. `README.md` - Added documentation
