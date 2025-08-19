* Add a new state for the thumbnail object `Archived`.
* During the slideshow, the shorcut "M" mark the thumbnail as `Archived` and skips to the next thumbnail. When an item is in this state it will not be selected for slideshows.
* The tagging is compatible with the slideshow undo function as the other marking shortcuts.
* When the background worker runs, videos in state `Archived` are moved to a custom folder `ArchiveDir`, the associated thumbnail and db entry are deleted.
* The folder is configured in the same way as `DataDir` folder.
* To move a video, first copy it and if there was no errors delete the original file.
* In case of errors during the copy, keep the current state of the thumbnail object and show a warning in the main page.
* A metric counts the total number of `Archived` items. On the main page, show the pending number of videos to be archived and the total number of archived videos. Do not show a preview of pendind archived videos on the main page like for the pending deletion videos.
* API endpoints for tagging videos as `Archived` and `Deleted` should be made availbale for external applications. This endpoint will receive the full name of the video; the path, if present, should be disregarded (use only the filename).

## API Specifications

### 1. Mark Video as Archived

```
POST /api/v1/video/archive
Content-Type: application/json

Request Body:
{
  "filename": "video_example.mp4"
}

Response (200 OK):
{
  "success": true,
  "message": "Video marked as archived",
  "filename": "video_example.mp4",
  "thumbnail_id": 123
}

Response (404 Not Found):
{
  "success": false,
  "error": "Video not found",
  "filename": "video_example.mp4"
}

Response (400 Bad Request):
{
  "success": false,
  "error": "Filename is required"
}
```

### 2. Mark Video for Deletion

```
POST /api/v1/video/delete
Content-Type: application/json

Request Body:
{
  "filename": "video_example.mp4"
}

Response (200 OK):
{
  "success": true,
  "message": "Video marked for deletion",
  "filename": "video_example.mp4",
  "thumbnail_id": 123
}

Response (404 Not Found):
{
  "success": false,
  "error": "Video not found",
  "filename": "video_example.mp4"
}

Response (409 Conflict):
{
  "success": false,
  "error": "Video is already marked for deletion",
  "filename": "video_example.mp4"
}
```

### 3. Get Video Status (Optional - for verification)

```
GET /api/v1/video/status/{filename}

Response (200 OK):
{
  "success": true,
  "filename": "video_example.mp4",
  "thumbnail_id": 123,
  "status": "archived",  // "success", "archived", "deleted", "pending", "error"
  "viewed": false,
  "created_at": "2025-08-19T10:30:00Z"
}

Response (404 Not Found):
{
  "success": false,
  "error": "Video not found",
  "filename": "video_example.mp4"
}
```

## Implementation Notes

1. **Database Changes Required**: Add `Archived` constant to models and update database queries to handle archived state
2. **Filename Matching**: API searches for thumbnails by `MovieFilename` field (not full path)
3. **Error Handling**: Consistent HTTP status codes and error response format
4. **Validation**: Filename parameter validation (required and non-empty)
5. **Logging**: Audit trail logging for all API operations
6. **Authentication**: Consider adding authentication/authorization if endpoints should be restricted

## Implementation Status

✅ **COMPLETED** - All features from this specification have been successfully implemented:

### Core Features Implemented:
- ✅ Added `StatusArchived` constant to models with validation
- ✅ Slideshow "M" shortcut for archiving with undo support  
- ✅ Archive directory configuration via `ARCHIVE_DIR` environment variable
- ✅ Background worker processing for archived items (copy then delete pattern)
- ✅ Manual processing button for immediate archival queue processing
- ✅ Database functions: `MarkForArchivalByID`, `GetArchivedThumbnails`
- ✅ Error handling and warning display for copy failures
- ✅ Archive metrics and statistics tracking
- ✅ UI integration with archive button and pending states
- ✅ CSS styling for archive functionality

### API Endpoints Implemented:
- ✅ `POST /api/v1/video/archive` - Mark video as archived by filename
- ✅ `POST /api/v1/video/delete` - Mark video for deletion by filename  
- ✅ `GET /api/v1/video/status/{filename}` - Get video status by filename
- ✅ Filename-based lookup using `GetByMovieFilename` function
- ✅ Consistent error responses and HTTP status codes
- ✅ Request/response validation and logging

### Technical Implementation:
- ✅ Updated `internal/models/models.go` with archived status
- ✅ Enhanced `internal/config/config.go` for archive directory
- ✅ Added database operations in `internal/database/database.go`
- ✅ Implemented handlers in `internal/server/handlers.go` (including manual processing)
- ✅ Background processing in `internal/scanner/scanner.go`
- ✅ UI updates in templates and JavaScript
- ✅ Metrics integration for monitoring

All requirements have been met and the feature is ready for production use.
