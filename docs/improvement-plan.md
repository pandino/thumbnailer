# Movie Thumbnailer Go - Improvement Plan

## Overview

This document outlines a comprehensive improvement plan for the Movie Thumbnailer Go application. The application is well-structured and functional, but several optimizations can enhance its performance, maintainability, and user experience.

## 1. Database Access Optimization

### Issues

- Multiple database queries where a single operation would suffice
- Lack of transactions for related operations
- Inefficient query patterns that could cause performance issues at scale

### Improvements

#### 1.1 Implement Upsert Operations

Replace separate existence checks and updates with single upsert operations. For example, in `scanner.go`:

```go
// Current approach
thumbnail, err := s.db.GetByMoviePath(movieFilename)
if err != nil {
    // Handle error
}
if thumbnail == nil {
    // Add new thumbnail
} else {
    // Update existing thumbnail
}

// Improved approach
err := s.db.UpsertThumbnail(thumbnail)
if err != nil {
    // Handle error
}
```

#### 1.2 Use Transactions for Related Operations

Add transaction support for operations that involve multiple database changes:

```go
// Add to database package
func (d *DB) WithTransaction(fn func(*sql.Tx) error) error {
    tx, err := d.db.Begin()
    if err != nil {
        return err
    }
    
    defer func() {
        if p := recover(); p != nil {
            tx.Rollback()
            panic(p) // re-throw panic after Rollback
        }
    }()
    
    if err := fn(tx); err != nil {
        tx.Rollback()
        return err
    }
    
    return tx.Commit()
}
```

Then use it for operations like cleanup:

```go
err := db.WithTransaction(func(tx *sql.Tx) error {
    // Delete thumbnails
    // Update records
    // Etc.
    return nil
})
```

#### 1.3 Optimize Query Patterns

- Add indices for frequently queried columns
- Use prepared statements for repeatedly executed queries
- Implement batch operations for bulk updates/inserts

```go
// Example: Using prepared statements
stmt, err := db.Prepare("UPDATE thumbnails SET viewed = ? WHERE id = ?")
if err != nil {
    return err
}
defer stmt.Close()

for _, id := range thumbnailIDs {
    if _, err := stmt.Exec(1, id); err != nil {
        return err
    }
}
```

#### 1.4 Implement Connection Pooling

Ensure the database connection pool is properly configured for the expected load:

```go
// In database.New()
db.SetMaxOpenConns(25)  // Adjust based on expected concurrent requests
db.SetMaxIdleConns(10)  // Keep some connections ready to use
db.SetConnMaxLifetime(time.Hour) // Recycle connections periodically
```

## 2. Error Handling Improvements

### Issues

- Inconsistent error handling patterns
- Some errors are logged but not returned
- Critical errors should have more visibility

### Improvements

#### 2.1 Standardize Error Handling

Create consistent error handling patterns throughout the codebase:

```go
// Helper function for domain errors
func DomainError(msg string, err error) error {
    return fmt.Errorf("%s: %w", msg, err)
}

// Usage
if err != nil {
    return DomainError("failed to process movie", err)
}
```

#### 2.2 Add Error Context

Always add context to errors to make debugging easier:

```go
// Instead of:
return err

// Do:
return fmt.Errorf("failed to process movie %s: %w", moviePath, err)
```

#### 2.3 Implement Proper Error Recovery

Add recovery mechanisms for background processes:

```go
defer func() {
    if r := recover(); r != nil {
        s.log.WithField("recover", r).Error("Panic in scan process")
        // Restart the process if needed
    }
}()
```

#### 2.4 Distinguish Between Expected and Unexpected Errors

Create a system to distinguish between normal errors (file not found) and unexpected errors (database corruption):

```go
type ErrorType int

const (
    ErrorTypeNormal ErrorType = iota
    ErrorTypeUnexpected
    ErrorTypeCritical
)

type AppError struct {
    Type    ErrorType
    Message string
    Err     error
}
```

## 3. Performance Optimizations

### Issues

- Inefficient FFmpeg parameter usage
- Suboptimal parallel processing
- Web interface loads all thumbnails at once

### Improvements

#### 3.1 Optimize FFmpeg Parameters

Fine-tune FFmpeg parameters for better performance and quality:

```go
// Current approach
cmd := exec.CommandContext(
    ctx,
    "ffmpeg",
    "-v", "verbose",
    "-ss", "30", // Skip first 30 seconds
    "-skip_frame", "nokey",
    "-i", moviePath,
    "-vf", fmt.Sprintf("select='eq(pict_type,I)',select='not(mod(n,%d))',scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2,tile=%dx%d:padding=4:margin=4",
        interval, t.cfg.GridCols, t.cfg.GridRows),
    "-frames:v", "1",
    "-q:v", "2",
    "-update", "1",
    "-y",
    outputPath,
)

// Optimized approach
cmd := exec.CommandContext(
    ctx,
    "ffmpeg",
    "-v", "error", // Only show errors
    "-threads", "2", // Limit threads
    "-ss", "30", // Skip first 30 seconds
    "-skip_frame", "nokey",
    "-i", moviePath,
    "-vf", fmt.Sprintf("select='eq(pict_type,I)',select='not(mod(n,%d))',scale=320:180:force_original_aspect_ratio=decrease:flags=fast_bilinear,pad=320:180:(ow-iw)/2:(oh-ih)/2,tile=%dx%d:padding=4:margin=4",
        interval, t.cfg.GridCols, t.cfg.GridRows),
    "-frames:v", "1",
    "-q:v", "3", // Slightly lower quality for faster encoding
    "-update", "1",
    "-y",
    outputPath,
)
```

#### 3.2 Improve Parallel Processing

Implement a more sophisticated work queue for thumbnail generation:

```go
// Create a buffered channel to limit concurrent work
type Job struct {
    MoviePath string
    Priority  int
}

workChan := make(chan Job, s.cfg.MaxWorkers*2)
results := make(chan error, s.cfg.MaxWorkers*2)

// Start workers
for i := 0; i < s.cfg.MaxWorkers; i++ {
    go func() {
        for job := range workChan {
            err := s.processMovie(ctx, job.MoviePath)
            results <- err
        }
    }()
}

// Queue jobs
for _, moviePath := range movieFiles {
    workChan <- Job{MoviePath: moviePath, Priority: 0}
}
close(workChan)

// Collect results
for i := 0; i < len(movieFiles); i++ {
    if err := <-results; err != nil {
        s.log.WithError(err).Error("Error processing movie")
    }
}
```

#### 3.3 Implement Pagination for Web Interface

Replace loading all thumbnails at once with pagination or infinite scrolling:

```javascript
// In app.js
let page = 1;
const pageSize = 20;

function loadThumbnails(containerId, status, viewed = null, page = 1) {
    const container = document.getElementById(containerId);
    if (!container) return;

    container.innerHTML = '<div class="loading">Loading...</div>';

    let url = `/api/thumbnails?status=${status}&page=${page}&pageSize=${pageSize}`;
    if (viewed !== null) {
        url += `&viewed=${viewed}`;
    }

    fetch(url)
        .then(response => response.json())
        .then(data => {
            renderThumbnails(container, data.thumbnails);
            
            // Add load more button if there are more thumbnails
            if (data.hasMore) {
                const loadMoreBtn = document.createElement('button');
                loadMoreBtn.className = 'load-more-btn';
                loadMoreBtn.textContent = 'Load More';
                loadMoreBtn.addEventListener('click', () => {
                    page++;
                    loadMoreThumbnails(containerId, status, viewed, page);
                });
                container.appendChild(loadMoreBtn);
            }
        })
        .catch(error => {
            console.error('Error fetching thumbnails:', error);
            container.innerHTML = `<div class="error">Failed to load thumbnails: ${error.message}</div>`;
        });
}

function loadMoreThumbnails(containerId, status, viewed = null, page = 1) {
    // Similar to loadThumbnails but appends to existing content
    // ...
}
```

#### 3.4 Implement Image Lazy Loading

Add lazy loading for images to improve page load performance:

```javascript
// In renderThumbnails function
const img = document.createElement('img');
img.setAttribute('data-src', `/thumbnails/${thumbnail.thumbnail_path}`);
img.setAttribute('alt', thumbnail.movie_filename);
img.classList.add('lazy-image');

// Initialize lazy loading
function initLazyLoading() {
    const lazyImages = document.querySelectorAll('.lazy-image');
    
    if ('IntersectionObserver' in window) {
        const observer = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    const img = entry.target;
                    img.src = img.getAttribute('data-src');
                    img.classList.remove('lazy-image');
                    observer.unobserve(img);
                }
            });
        });
        
        lazyImages.forEach(img => observer.observe(img));
    } else {
        // Fallback for browsers without IntersectionObserver
        lazyImages.forEach(img => {
            img.src = img.getAttribute('data-src');
            img.classList.remove('lazy-image');
        });
    }
}
```

## 4. Code Organization and Maintainability

### Issues

- Some functions are too long
- Repeated code patterns
- Inconsistent separation of concerns

### Improvements

#### 4.1 Break Down Long Functions

Split long functions into smaller, more focused ones. For example, in `server/handlers.go`, the `handleSlideshow` function:

```go
// Current: One long function
func (s *Server) handleSlideshow(w http.ResponseWriter, r *http.Request) {
    // Over 100 lines of code
}

// Improved: Split into smaller functions
func (s *Server) handleSlideshow(w http.ResponseWriter, r *http.Request) {
    session, err := s.getOrCreateSlideshowSession(w, r)
    if err != nil {
        s.handleSlideshowError(w, r, err)
        return
    }
    
    thumbnail, err := s.getSlideshowThumbnail(r, session)
    if err != nil {
        s.handleSlideshowError(w, r, err)
        return
    }
    
    if thumbnail == nil {
        s.handleNoThumbnailsFound(w, r)
        return
    }
    
    session = s.updateSessionWithThumbnail(w, session, thumbnail)
    s.renderSlideshowPage(w, r, thumbnail, session)
}

func (s *Server) getOrCreateSlideshowSession(w http.ResponseWriter, r *http.Request) (*SessionData, error) {
    // Logic to get or create session
}

// Additional helper functions...
```

#### 4.2 Extract Common Patterns to Helper Functions

Identify repeated code patterns and extract them to helper functions:

```go
// Helper function for setting flash messages
func (s *Server) setFlashMessage(w http.ResponseWriter, msg string, msgType string) {
    value := msg
    if msgType != "" {
        value = msgType + ":" + msg
    }
    http.SetCookie(w, &http.Cookie{
        Name:  "flash",
        Value: url.QueryEscape(value),
        Path:  "/",
    })
}

// Helper function for JSON responses
func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}, statusCode int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    if err := json.NewEncoder(w).Encode(data); err != nil {
        s.log.WithError(err).Error("Failed to encode JSON response")
    }
}
```

#### 4.3 Improve Separation of Concerns

Better separate different responsibilities in the codebase:

```go
// Create a ThumbnailService to encapsulate business logic
type ThumbnailService struct {
    db      *database.DB
    scanner *scanner.Scanner
    log     *logrus.Logger
}

func NewThumbnailService(db *database.DB, scanner *scanner.Scanner, log *logrus.Logger) *ThumbnailService {
    return &ThumbnailService{
        db:      db,
        scanner: scanner,
        log:     log,
    }
}

func (s *ThumbnailService) GetRandomUnviewed() (*models.Thumbnail, error) {
    // Business logic for getting a random unviewed thumbnail
}

func (s *ThumbnailService) MarkAsViewed(id int64) error {
    // Business logic for marking as viewed
}

// Then use in handlers
func (s *Server) handleMarkViewed(w http.ResponseWriter, r *http.Request) {
    id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
    if err != nil {
        s.handleError(w, r, "Invalid ID", http.StatusBadRequest)
        return
    }
    
    err = s.thumbnailService.MarkAsViewed(id)
    if err != nil {
        s.handleError(w, r, "Failed to mark as viewed", http.StatusInternalServerError)
        return
    }
    
    s.jsonResponse(w, map[string]bool{"success": true}, http.StatusOK)
}
```

## 5. Additional Improvements

### 5.1 Implement Proper Logging Strategy

Improve logging with structured fields and appropriate log levels:

```go
// Configure log format for JSON in production
if !cfg.Debug {
    log.SetFormatter(&logrus.JSONFormatter{})
}

// Use appropriate log levels
s.log.WithFields(logrus.Fields{
    "movie": moviePath,
    "duration": metadata.Duration,
    "resolution": fmt.Sprintf("%dx%d", metadata.Width, metadata.Height),
}).Info("Processing movie")
```

### 5.2 Add Health Check Endpoint

Implement a health check endpoint for monitoring:

```go
s.router.HandleFunc("/health", s.handleHealthCheck).Methods("GET")

func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
    status := map[string]interface{}{
        "status": "ok",
        "version": "1.0.0",
        "uptime": time.Since(s.startTime).String(),
        "databaseConnected": s.isDatabaseConnected(),
        "isScanning": s.scanner.IsScanning(),
    }
    
    s.jsonResponse(w, status, http.StatusOK)
}
```

### 5.3 Add Unit and Integration Tests

Implement comprehensive testing to ensure reliability:

```go
// Example test for database functions
func TestDatabaseGetByID(t *testing.T) {
    // Set up test database
    tempDB, err := database.New(":memory:")
    if err != nil {
        t.Fatalf("Failed to create test database: %v", err)
    }
    defer tempDB.Close()
    
    // Insert test data
    thumbnail := &models.Thumbnail{
        MoviePath: "test.mp4",
        MovieFilename: "test.mp4",
        ThumbnailPath: "test.jpg",
        Status: models.StatusSuccess,
    }
    if err := tempDB.Add(thumbnail); err != nil {
        t.Fatalf("Failed to add test data: %v", err)
    }
    
    // Test GetByID function
    result, err := tempDB.GetByID(1)
    if err != nil {
        t.Fatalf("GetByID failed: %v", err)
    }
    
    if result.MoviePath != "test.mp4" {
        t.Errorf("Expected MoviePath 'test.mp4', got '%s'", result.MoviePath)
    }
}
```

### 5.4 Improve User Experience

Add features to enhance the user experience:

- Add search functionality for thumbnails
- Implement sorting options (by date, duration, name)
- Add filtering options (by resolution, duration range)
- Improve mobile responsiveness

### 5.5 Security Enhancements

Strengthen the application's security:

- Implement CSRF protection
- Add rate limiting for API endpoints
- Validate all user inputs
- Implement secure cookie handling
- Add authentication if needed

## Implementation Plan

To implement these improvements effectively, we recommend the following phased approach:

### Phase 1: Critical Performance Improvements (1-2 weeks)
- Database optimizations (upsert, transactions)
- Error handling standardization
- Basic code reorganization

### Phase 2: User Experience Improvements (2-3 weeks)
- Pagination and lazy loading
- UI/UX enhancements
- Improved parallel processing

### Phase 3: Long-term Maintainability (3-4 weeks)
- Comprehensive testing
- Security enhancements
- Advanced logging and monitoring

## Conclusion

By implementing these improvements, the Movie Thumbnailer Go application will be more efficient, maintainable, and user-friendly. The changes preserve the application's core functionality while enhancing its performance and reliability.
