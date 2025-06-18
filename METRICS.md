# Movie Thumbnailer Prometheus Metrics

This document describes the Prometheus metrics exposed by the Movie Thumbnailer application at the `/metrics` endpoint.

## Implemented Metrics

### HTTP Metrics
- **`movie_thumbnailer_http_requests_total`** (Counter with labels: method, endpoint, status_code)
  - Total number of HTTP requests processed
  - Useful for monitoring API usage and error rates

- **`movie_thumbnailer_http_request_duration_seconds`** (Histogram with labels: method, endpoint)
  - Duration of HTTP requests in seconds
  - Useful for monitoring API performance and response times

- **`movie_thumbnailer_http_active_connections`** (Gauge)
  - Number of active HTTP connections
  - Useful for monitoring current load

### Application Metrics
- **`movie_thumbnailer_thumbnails_total`** (Gauge with label: status)
  - Total number of thumbnails by status (success, error, pending, deleted)
  - Key business metric for monitoring processing state

- **`movie_thumbnailer_thumbnail_generation_total`** (Counter with label: result)
  - Total number of thumbnail generation attempts
  - Useful for monitoring processing volume and success rate

- **`movie_thumbnailer_thumbnail_generation_duration_seconds`** (Histogram)
  - Duration of thumbnail generation operations
  - Custom buckets: [1, 5, 10, 30, 60, 120, 300] seconds
  - Useful for monitoring FFmpeg processing performance

### Scanning Metrics
- **`movie_thumbnailer_scan_operations_total`** (Counter with label: result)
  - Total number of scanning operations (success/error)
  - Useful for monitoring scan reliability

- **`movie_thumbnailer_scan_duration_seconds`** (Histogram)
  - Duration of scanning operations
  - Custom buckets: [1, 5, 10, 30, 60, 300, 600] seconds
  - Useful for monitoring scan performance

- **`movie_thumbnailer_last_scan_timestamp`** (Gauge)
  - Unix timestamp of the last successful scan
  - Useful for alerting on stale scans

### Slideshow Metrics
- **`movie_thumbnailer_slideshow_sessions_total`** (Counter with label: result)
  - Total number of slideshow sessions (completed, deleted_and_completed)
  - Useful for monitoring user engagement

- **`movie_thumbnailer_slideshow_session_duration_seconds`** (Histogram)
  - Duration of slideshow sessions
  - Custom buckets: [10, 30, 60, 300, 600, 1800, 3600] seconds
  - Useful for understanding user behavior

- **`movie_thumbnailer_slideshow_views_total`** (Counter)
  - Total number of images viewed in slideshow
  - Key user engagement metric

### Storage Metrics
- **`movie_thumbnailer_total_file_size_bytes`** (Gauge with label: category)
  - Total file size in bytes by category (viewed, unviewed)
  - Useful for storage monitoring and cleanup planning

### Worker Metrics
- **`movie_thumbnailer_background_tasks_total`** (Counter with labels: task_type, result)
  - Total number of background tasks executed
  - Task types: initial_scan, scheduled_scan, manual_scan, cleanup
  - Useful for monitoring background job health

- **`movie_thumbnailer_worker_errors_total`** (Counter with labels: worker_type, error_type)
  - Total number of worker errors
  - Useful for error monitoring and alerting

### FFmpeg Metrics
- **`movie_thumbnailer_ffmpeg_executions_total`** (Counter with label: result)
  - Total number of FFmpeg executions (success/error)
  - Useful for monitoring FFmpeg reliability

- **`movie_thumbnailer_ffmpeg_duration_seconds`** (Histogram)
  - Duration of FFmpeg executions
  - Custom buckets: [0.5, 1, 2, 5, 10, 30, 60] seconds
  - Useful for monitoring FFmpeg performance

## Usage Examples

### Monitoring Dashboard Queries

**Thumbnail Processing Rate:**
```promql
rate(movie_thumbnailer_thumbnail_generation_total[5m])
```

**Error Rate:**
```promql
rate(movie_thumbnailer_http_requests_total{status_code!~"2.."}[5m]) / rate(movie_thumbnailer_http_requests_total[5m])
```

**Average Response Time:**
```promql
rate(movie_thumbnailer_http_request_duration_seconds_sum[5m]) / rate(movie_thumbnailer_http_request_duration_seconds_count[5m])
```

**Storage Usage:**
```promql
movie_thumbnailer_total_file_size_bytes
```

### Alerting Rules

**Scan Failure Alert:**
```promql
increase(movie_thumbnailer_scan_operations_total{result="error"}[1h]) > 0
```

**Stale Scan Alert:**
```promql
time() - movie_thumbnailer_last_scan_timestamp > 3600
```

**High Error Rate Alert:**
```promql
rate(movie_thumbnailer_thumbnail_generation_total{result="error"}[5m]) / rate(movie_thumbnailer_thumbnail_generation_total[5m]) > 0.1
```

## Accessing Metrics

The metrics are available at: `http://localhost:8080/metrics`

The endpoint returns standard Prometheus format metrics that can be scraped by Prometheus server or viewed directly in a browser.

## Implementation Notes

- Metrics are updated in real-time as operations occur
- Database stats (thumbnail counts, file sizes) are updated every 30 seconds
- All duration metrics use custom histogram buckets appropriate for the operation type
- Labels are used to provide dimensional data for better monitoring granularity
