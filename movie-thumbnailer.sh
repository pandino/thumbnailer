#!/bin/bash
#
# Movie Thumbnailer - Complete Version with Full Cleanup
#

# Configuration
INPUT_DIR="${MOVIE_INPUT_DIR:-/movies}"
OUTPUT_DIR="${THUMBNAIL_OUTPUT_DIR:-/thumbnails}"
DATA_DIR="${DATA_DIR:-/data}"
DB_FILE="${DB_FILE:-$DATA_DIR/thumbnailer.db}"
GRID_COLS="${GRID_COLS:-8}"
GRID_ROWS="${GRID_ROWS:-4}"
MAX_WORKERS="${MAX_WORKERS:-4}"
FILE_EXTENSIONS="${FILE_EXTENSIONS:-mp4 mkv avi mov mts wmv}"

# Debug flag - set this to "true" to enable debugging and log file creation
DEBUG="${DEBUG:-false}"

# For verbose debugging
if [ "$DEBUG" = "true" ]; then
  set -x
else
  set +x
fi

# Function to log messages with correct PID handling
log() {
  local message="$1"
  local pid_prefix=""
  
  # Use actual PID of the current process unless overridden
  local actual_pid=$$
  if [ -n "$2" ]; then
    actual_pid="$2"
  fi
  
  pid_prefix="[PID:$actual_pid] "
  
  # Format the message with timestamp and PID
  local formatted_message="[$(date '+%Y-%m-%d %H:%M:%S')] ${pid_prefix}$message"
  
  # Only write to log file if in debug mode
  if [ "$DEBUG" = "true" ]; then
    echo "$formatted_message" >> "$OUTPUT_DIR/thumbnailer.log"
  fi
  
  # Always display to stdout for container logs
  echo "$formatted_message"
}

# Setup directories
mkdir -p "$OUTPUT_DIR"
mkdir -p "$DATA_DIR"

# Only create log file if in debug mode
if [ "$DEBUG" = "true" ]; then
  touch "$OUTPUT_DIR/thumbnailer.log"
fi

log "Script started with PID $$"

# Check for required tools
for tool in ffmpeg ffprobe bc sqlite3; do
  if ! command -v $tool &> /dev/null; then
    log "ERROR: Required tool '$tool' is not installed"
    exit 1
  else
    log "Found required tool: $tool at $(which $tool)"
  fi
done

# Initialize SQLite database
init_database() {
  log "Initializing SQLite database at $DB_FILE"
  
  # Create database if it doesn't exist
  sqlite3 "$DB_FILE" "
    CREATE TABLE IF NOT EXISTS thumbnails (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      movie_path TEXT UNIQUE,
      movie_filename TEXT,
      thumbnail_path TEXT,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
      status TEXT DEFAULT 'pending'
    );
  "
  
  if [ $? -ne 0 ]; then
    log "ERROR: Failed to initialize database"
    exit 1
  else
    log "Database initialized successfully"
  fi
}

# Function to add a movie to the database
add_to_database() {
  local movie_path="$1"
  local thumbnail_path="$2"
  local filename=$(basename "$movie_path")
  local actual_pid=$$
  
  log "Adding to database: $filename" "$actual_pid"
  
  sqlite3 "$DB_FILE" "
    INSERT OR REPLACE INTO thumbnails (movie_path, movie_filename, thumbnail_path, status)
    VALUES ('$movie_path', '$filename', '$thumbnail_path', 'pending');
  "
  
  if [ $? -ne 0 ]; then
    log "ERROR: Failed to add $filename to database" "$actual_pid"
    return 1
  else
    log "Added $filename to database" "$actual_pid"
    return 0
  fi
}

# Function to update status in the database
update_status() {
  local movie_path="$1"
  local status="$2"
  local actual_pid=$$
  
  log "Updating status for $(basename "$movie_path") to $status" "$actual_pid"
  
  sqlite3 "$DB_FILE" "
    UPDATE thumbnails 
    SET status = '$status'
    WHERE movie_path = '$movie_path';
  "
  
  if [ $? -ne 0 ]; then
    log "ERROR: Failed to update status for $(basename "$movie_path")" "$actual_pid"
    return 1
  else
    log "Updated status for $(basename "$movie_path")" "$actual_pid"
    return 0
  fi
}

# Function to remove an entry from the database and associated thumbnail
remove_from_database() {
  local movie_path="$1"
  local actual_pid=$$
  local delete_thumbnail="${2:-true}"  # Default to deleting thumbnail
  
  # Get the thumbnail path first before deleting the database entry
  local thumbnail_path=$(sqlite3 "$DB_FILE" "SELECT thumbnail_path FROM thumbnails WHERE movie_path = '$movie_path';")
  
  log "Removing from database: $(basename "$movie_path")" "$actual_pid"
  
  # Delete from database
  sqlite3 "$DB_FILE" "
    DELETE FROM thumbnails WHERE movie_path = '$movie_path';
  "
  
  if [ $? -ne 0 ]; then
    log "ERROR: Failed to remove $(basename "$movie_path") from database" "$actual_pid"
    return 1
  else
    log "Removed $(basename "$movie_path") from database" "$actual_pid"
    
    # If thumbnail deletion is requested and the thumbnail exists, delete it
    if [ "$delete_thumbnail" = "true" ] && [ -n "$thumbnail_path" ] && [ -f "$thumbnail_path" ]; then
      log "Deleting associated thumbnail: $(basename "$thumbnail_path")" "$actual_pid"
      if rm -f "$thumbnail_path"; then
        log "Successfully deleted thumbnail: $(basename "$thumbnail_path")" "$actual_pid"
      else
        log "ERROR: Failed to delete thumbnail: $(basename "$thumbnail_path")" "$actual_pid"
      fi
    fi
    
    return 0
  fi
}

# Function to process a single movie
create_thumbnail() {
  local movie_path="$1"
  local filename=$(basename "$movie_path")
  local output_path="$OUTPUT_DIR/${filename%.*}.jpg"
  local actual_pid=$$
  
  # Log with actual PID
  log "Creating thumbnail for: $filename (Process PID: $actual_pid)" "$actual_pid"
  
  # Check if the file exists and is readable
  if [ ! -r "$movie_path" ]; then
    log "ERROR: Cannot read file: $movie_path" "$actual_pid"
    update_status "$movie_path" "error"
    return 1
  fi
  
  # Add to database first
  add_to_database "$movie_path" "$output_path"
  
  # Count keyframes (I-frames) using direct packet flags
  log "Counting keyframes for $filename..." "$actual_pid"
  local keyframe_count=0
  local ffprobe_error_log
  
  # Create temporary error log only in debug mode
  if [ "$DEBUG" = "true" ]; then
    ffprobe_error_log="$OUTPUT_DIR/${filename}.ffprobe.log"
  else
    ffprobe_error_log="/dev/null"
  fi
  
  # Get keyframe count using packet flags with better error handling
  if ! keyframe_count=$(ffprobe -v error -select_streams v:0 -show_entries packet=flags -of csv "$movie_path" 2>"$ffprobe_error_log" | grep -c "K"); then
    log "WARNING: ffprobe failed for $filename" "$actual_pid"
    # Cat the error log to the main log if in debug mode
    if [ "$DEBUG" = "true" ] && [ -f "$ffprobe_error_log" ]; then
      log "ffprobe error details for $filename:" "$actual_pid"
      cat "$ffprobe_error_log" >&2
    fi
    keyframe_count=100
  fi
  
  if [ "$keyframe_count" -gt 0 ]; then
    log "Found $keyframe_count keyframes in the video: $filename" "$actual_pid"
  else
    log "No keyframes found in $filename, using default count" "$actual_pid"
    keyframe_count=100
  fi
  
  # Calculate interval to distribute frames across grid
  local interval=10  # Default fallback
  
  if [ "$keyframe_count" -gt 0 ]; then
    interval=$((keyframe_count / (GRID_COLS * GRID_ROWS) + 1))
    log "Using keyframe interval: $interval for $filename" "$actual_pid"
  fi
  
  # Create a temporary error log
  local error_log
  if [ "$DEBUG" = "true" ]; then
    error_log="$OUTPUT_DIR/${filename%.*}.error.log"
  else
    error_log="/dev/null"
  fi
  
  # Create the thumbnail using ffmpeg with verbose logging
  log "Creating thumbnail grid for $filename (output: $output_path)..." "$actual_pid"
  
  # Check output directory is writable
  if [ ! -w "$OUTPUT_DIR" ]; then
    log "ERROR: Output directory $OUTPUT_DIR is not writable!" "$actual_pid"
    update_status "$movie_path" "error"
    return 1
  fi
  
  # Try to create a test file in the output directory
  if ! touch "$OUTPUT_DIR/test_write_$$.tmp" 2>/dev/null; then
    log "ERROR: Cannot write to output directory $OUTPUT_DIR!" "$actual_pid"
    update_status "$movie_path" "error"
    return 1
  else
    # Remove the test file
    rm "$OUTPUT_DIR/test_write_$$.tmp"
    log "Confirmed write access to $OUTPUT_DIR" "$actual_pid"
  fi
  
  # Run ffmpeg with the necessary options for thumbnail grid creation
  # The -update 1 parameter is critical for writing the single output frame
  if ffmpeg -v verbose -skip_frame nokey -i "$movie_path" \
    -vf "select='eq(pict_type,I)',select='not(mod(n,$interval))',scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2,tile=${GRID_COLS}x${GRID_ROWS}:padding=4:margin=4" \
    -frames:v 1 -q:v 2 -update 1 -y "$output_path" 2>"$error_log"; then
    
    # Verify file was created
    if [ -f "$output_path" ]; then
      local file_size=$(stat -c %s "$output_path")
      log "SUCCESS: Created thumbnail for: $filename at $output_path" "$actual_pid"
      log "Thumbnail size: $file_size bytes" "$actual_pid"
      update_status "$movie_path" "success"
      return 0
    else
      log "ERROR: ffmpeg reported success but no file was created at $output_path" "$actual_pid"
      update_status "$movie_path" "error"
      return 1
    fi
  else
    local exit_code=$?
    log "ERROR: Failed to create thumbnail for: $filename. Exit code: $exit_code" "$actual_pid"
    # Print the error log content if in debug mode
    if [ "$DEBUG" = "true" ] && [ -f "$error_log" ]; then
      log "ffmpeg error details for $filename:" "$actual_pid"
      cat "$error_log" >&2
    fi
    update_status "$movie_path" "error"
    return 1
  fi
}

# Function to clean up database entries and thumbnails for missing movie files
cleanup_database() {
  log "Cleaning up database entries for missing movie files..."
  
  # Get all movies from database
  local movies=$(sqlite3 "$DB_FILE" "SELECT movie_path, thumbnail_path FROM thumbnails WHERE status != 'deleted';")
  
  if [ -z "$movies" ]; then
    log "No movies to clean up in database"
    return 0
  fi
  
  local removed_count=0
  local thumbnail_count=0
  
  # Check each movie
  while IFS='|' read -r movie_path thumbnail_path; do
    # Skip if empty
    [ -z "$movie_path" ] && continue
    
    # Check if movie file exists
    if [ ! -f "$movie_path" ]; then
      local filename=$(basename "$movie_path")
      log "Movie file not found: $filename, removing from database and cleaning up"
      
      # Delete the thumbnail if it exists
      if [ -n "$thumbnail_path" ] && [ -f "$thumbnail_path" ]; then
        log "Deleting orphaned thumbnail: $(basename "$thumbnail_path")"
        if rm -f "$thumbnail_path"; then
          log "Successfully deleted orphaned thumbnail: $(basename "$thumbnail_path")"
          thumbnail_count=$((thumbnail_count + 1))
        else
          log "ERROR: Failed to delete orphaned thumbnail: $(basename "$thumbnail_path")"
        fi
      fi
      
      # Remove from database
      remove_from_database "$movie_path" "false" # Skip thumbnail deletion since we've already handled it
      removed_count=$((removed_count + 1))
    fi
  done <<< "$movies"
  
  log "Database cleanup complete. Removed $removed_count entries for missing movie files and deleted $thumbnail_count orphaned thumbnails."
}

# Function to clean up orphaned thumbnails (thumbnails without a corresponding movie)
cleanup_orphaned_thumbnails() {
  log "Checking for orphaned thumbnails..."
  
  # Get all thumbnails from the database
  local thumbs=$(sqlite3 "$DB_FILE" "SELECT thumbnail_path FROM thumbnails;")
  
  # Create a temporary file to store database thumbnails
  local db_thumbs_file=$(mktemp)
  echo "$thumbs" > "$db_thumbs_file"
  
  # Find all thumbnails in the output directory
  local found_thumbs=$(find "$OUTPUT_DIR" -type f -name "*.jpg" | sort)
  local orphaned_count=0
  
  # Check each thumbnail
  while read -r thumbnail_path; do
    # Skip if empty
    [ -z "$thumbnail_path" ] && continue
    
    # Check if thumbnail is in the database
    if ! grep -q "^$thumbnail_path$" "$db_thumbs_file"; then
      local thumb_filename=$(basename "$thumbnail_path")
      log "Orphaned thumbnail found: $thumb_filename, deleting"
      
      if rm -f "$thumbnail_path"; then
        log "Successfully deleted orphaned thumbnail: $thumb_filename"
        orphaned_count=$((orphaned_count + 1))
      else
        log "ERROR: Failed to delete orphaned thumbnail: $thumb_filename"
      fi
    fi
  done <<< "$found_thumbs"
  
  # Clean up temporary file
  rm -f "$db_thumbs_file"
  
  log "Orphaned thumbnail cleanup complete. Deleted $orphaned_count orphaned thumbnails."
}

# Function to process files in parallel (enhanced implementation)
process_in_parallel() {
  local max_workers=$MAX_WORKERS
  local running=0
  local pids=()
  local statuses=()
  local files=()
  
  log "Processing movies with max $max_workers parallel jobs"
  
  # First, clean up database entries for missing movie files
  cleanup_database
  
  # Count the total files to process
  local total_files=0
  
  # Normalize extensions to lowercase
  local extensions_list=()
  for ext in $FILE_EXTENSIONS; do
    extensions_list+=("$(echo "$ext" | tr '[:upper:]' '[:lower:]')")
  done
  
  # Count files with each extension (case insensitive)
  for ext in "${extensions_list[@]}"; do
    # Use case-insensitive find
    local count=$(find "$INPUT_DIR" -type f -iname "*.${ext}" | wc -l)
    total_files=$((total_files + count))
    log "Found $count files with extension .$ext"
  done
  
  if [ $total_files -eq 0 ]; then
    log "WARNING: No movie files found in $INPUT_DIR"
    return 0
  fi
  
  log "Found $total_files movie files to process"
  
  # Create an array to store all files for better process tracking
  all_files=()
  
  # Find all movie files first
  for ext in "${extensions_list[@]}"; do
    while read -r movie; do
      # Skip if empty or no matches found
      [ -z "$movie" ] || [ ! -f "$movie" ] && continue
      all_files+=("$movie")
    done < <(find "$INPUT_DIR" -type f -iname "*.${ext}" 2>/dev/null)
  done
  
  log "Built list of ${#all_files[@]} files to process"
  
  # Process all files in the array
  for movie in "${all_files[@]}"; do
    # Skip if file doesn't exist
    [ ! -f "$movie" ] && continue
    
    local filename=$(basename "$movie")
    
    # Check if thumbnail already exists in the database
    local status=$(sqlite3 "$DB_FILE" "SELECT status FROM thumbnails WHERE movie_path = '$movie';" 2>/dev/null)
    
    if [ -n "$status" ] && [ "$status" = "success" ]; then
      log "Skipping $filename, already in database with status: $status"
      continue
    fi
    
    # Wait if at max workers
    while [ $running -ge $max_workers ]; do
      log "At max workers ($running/$max_workers), waiting for a process to complete..."
      local completed=0
      
      for i in "${!pids[@]}"; do
        if [ -n "${pids[$i]}" ] && ! kill -0 ${pids[$i]} 2>/dev/null; then
          # Process has finished
          wait ${pids[$i]} 2>/dev/null
          local exit_status=$?
          log "Process ${pids[$i]} for $(basename "${files[$i]}") completed with status $exit_status"
          
          # Check if the thumbnail was created
          local output_path="$OUTPUT_DIR/$(basename "${files[$i]%.*}").jpg"
          if [ -f "$output_path" ]; then
            log "Verified thumbnail exists for $(basename "${files[$i]}"): $output_path"
            log "Thumbnail file size: $(stat -c %s "$output_path") bytes"
          else
            log "ERROR: No thumbnail created for $(basename "${files[$i]}") at $output_path"
          fi
          
          # Clear this slot
          unset pids[$i]
          unset files[$i]
          completed=1
          ((running--))
        fi
      done
      
      # Re-index arrays if we unset any elements
      if [ $completed -eq 1 ]; then
        pids=("${pids[@]}")
        files=("${files[@]}")
      else
        # If no process completed in this iteration, sleep to avoid busy waiting
        sleep 1
      fi
    done
    
    # Process in background (normal mode) or foreground (debug mode)
    log "Starting: $filename"
    
    if [ "$DEBUG" = "true" ]; then
      log "DEBUG MODE: Processing $filename in foreground for debugging"
      create_thumbnail "$movie"
    else
      # Process in background for parallel operation
      create_thumbnail "$movie" &
      local pid=$!
      pids+=($pid)
      files+=("$movie")
      ((running++))
      log "Started process $pid for $filename, running: $running/$max_workers"
    fi
  done
  
  log "All jobs submitted, waiting for remaining processes to complete..."
  
  # Wait for remaining processes - more robust implementation
  for i in "${!pids[@]}"; do
    if [ -n "${pids[$i]}" ]; then
      log "Waiting for process ${pids[$i]} ($(basename "${files[$i]}"))..."
      wait ${pids[$i]} 2>/dev/null
      local exit_status=$?
      
      if [ $exit_status -ne 0 ]; then
        log "WARNING: Process ${pids[$i]} for $(basename "${files[$i]}") exited with status $exit_status"
      else
        log "Process ${pids[$i]} for $(basename "${files[$i]}") completed successfully"
      fi
      
      # Verify the thumbnail was actually created
      local output_path="$OUTPUT_DIR/$(basename "${files[$i]%.*}").jpg"
      if [ -f "$output_path" ]; then
        log "Verified thumbnail exists for $(basename "${files[$i]}"): $output_path"
        log "Thumbnail file size: $(stat -c %s "$output_path") bytes"
      else
        log "ERROR: No thumbnail created for $(basename "${files[$i]}") at $output_path"
      fi
    fi
  done
  
  # Cleanup orphaned thumbnails after processing
  cleanup_orphaned_thumbnails
  
  log "Parallel processing completed for $total_files files"
}

# Function to verify thumbnails and optionally delete movies with missing thumbnails
verify_thumbnails() {
  local delete_mode="$1"
  
  log "Verifying thumbnails..."
  
  # First, clean up database entries for missing movie files
  cleanup_database
  
  # Get all movies from database that aren't already marked as deleted
  local movies=$(sqlite3 "$DB_FILE" "SELECT movie_path, thumbnail_path FROM thumbnails WHERE status != 'deleted';")
  
  if [ -z "$movies" ]; then
    log "No thumbnails found in database"
    return 0
  fi
  
  local missing_count=0
  local deleted_count=0
  local missing_files=""
  
  # Check each movie
  while IFS='|' read -r movie_path thumbnail_path; do
    # Skip if empty
    [ -z "$movie_path" ] && continue
    
    # Skip if movie file doesn't exist - we've already cleaned these up
    if [ ! -f "$movie_path" ]; then
      continue
    fi
    
    # Check if thumbnail exists
    if [ ! -f "$thumbnail_path" ]; then
      local filename=$(basename "$movie_path")
      log "Missing thumbnail for $filename"
      missing_count=$((missing_count + 1))
      missing_files="${missing_files}${filename}\n"
      
      # Update status in database to 'missing' only if it's not already marked as deleted or missing
      local current_status=$(sqlite3 "$DB_FILE" "SELECT status FROM thumbnails WHERE movie_path = '$movie_path';")
      if [ "$current_status" != "deleted" ] && [ "$current_status" != "missing" ]; then
        sqlite3 "$DB_FILE" "UPDATE thumbnails SET status = 'missing' WHERE movie_path = '$movie_path';"
      fi
      
      # Delete the movie if requested
      if [ "$delete_mode" = "delete" ]; then
        log "Deleting movie: $movie_path"
        if rm -f "$movie_path"; then
          log "Successfully deleted: $movie_path"
          deleted_count=$((deleted_count + 1))
          # Remove completely from database
          remove_from_database "$movie_path" "false" # Skip thumbnail deletion since there's no thumbnail
        else
          log "Failed to delete: $movie_path"
        fi
      fi
    fi
  done <<< "$movies"
  
  log "Verification complete. Found $missing_count missing thumbnails."
  if [ $missing_count -gt 0 ] && [ -n "$missing_files" ]; then
    log "Missing thumbnails for:"
    echo -e "$missing_files" | while read -r line; do
      [ -n "$line" ] && log "  - $line"
    done
  fi
  
  if [ "$delete_mode" = "delete" ]; then
    log "Deleted $deleted_count movies with missing thumbnails."
  fi
  
  # Clean up orphaned thumbnails
  cleanup_orphaned_thumbnails
}

# Function to print database statistics
print_stats() {
  log "Database statistics:"
  
  # First, clean up database entries for missing movie files
  cleanup_database
  
  local total=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails;")
  local success=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails WHERE status = 'success';")
  local error=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails WHERE status = 'error';")
  local missing=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails WHERE status = 'missing';")
  local deleted=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails WHERE status = 'deleted';")
  local pending=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails WHERE status = 'pending';")
  
  log "Total movies: $total"
  log "Successful thumbnails: $success"
  log "Failed thumbnails: $error"
  log "Missing thumbnails: $missing"
  log "Deleted movies: $deleted"
  log "Pending thumbnails: $pending"
  
  # Count thumbnails in output directory
  local thumbs_count=$(find "$OUTPUT_DIR" -type f -name "*.jpg" | wc -l)
  log "Actual thumbnail files: $thumbs_count"
  
  # Check for orphaned thumbnails
  local db_thumbs=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails WHERE thumbnail_path LIKE '%jpg' AND status != 'deleted';")
  if [ $thumbs_count -gt $db_thumbs ]; then
    local orphaned=$((thumbs_count - db_thumbs))
    log "WARNING: Found $orphaned orphaned thumbnails not tracked in database."
    log "Run 'cleanup' command to remove orphaned thumbnails."
  fi
}

# Function to clean up everything (thumbnails and database)
cleanup_everything() {
  log "Starting complete cleanup..."
  
  # Clean up database entries for missing movies and their thumbnails
  cleanup_database
  
  # Clean up orphaned thumbnails
  cleanup_orphaned_thumbnails
  
  # Clean up deleted entries
  local deleted_count=$(sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM thumbnails WHERE status = 'deleted';")
  if [ $deleted_count -gt 0 ]; then
    log "Removing $deleted_count entries marked as 'deleted' from database"
    sqlite3 "$DB_FILE" "DELETE FROM thumbnails WHERE status = 'deleted';"
    log "Successfully removed deleted entries from database"
  fi
  
  # Vacuum the database to reclaim space
  log "Optimizing database..."
  sqlite3 "$DB_FILE" "VACUUM;"
  log "Database optimization complete"
  
  log "Cleanup completed successfully"
}

# Helper function to display usage
display_help() {
  echo "Movie Thumbnailer - A tool to create thumbnail grids from movies"
  echo ""
  echo "Usage: $0 [command]"
  echo ""
  echo "Commands:"
  echo "  scan           - Process movies and create thumbnails (default)"
  echo "  verify         - Check for missing thumbnails"
  echo "  delete-missing - Delete movies with missing thumbnails"
  echo "  stats          - Show database statistics"
  echo "  cleanup        - Clean up orphaned thumbnails and optimize database"
  echo "  help           - Show this help"
  echo ""
  echo "Environment Variables:"
  echo "  MOVIE_INPUT_DIR       - Directory containing the movies (default: /movies)"
  echo "  THUMBNAIL_OUTPUT_DIR  - Directory to store generated thumbnails (default: /thumbnails)"
  echo "  DATA_DIR              - Directory for the database (default: /data)"
  echo "  MAX_WORKERS           - Maximum number of parallel processes (default: 4)"
  echo "  GRID_COLS             - Number of columns in the thumbnail grid (default: 8)"
  echo "  GRID_ROWS             - Number of rows in the thumbnail grid (default: 4)"
  echo "  DEBUG                 - Enable debug output and log file creation (default: false)"
  echo ""
  echo "Example:"
  echo "  $0 scan               - Process all movies in $INPUT_DIR"
  echo "  DEBUG=true $0 scan    - Process with debugging enabled and log file creation"
  echo ""
}

# Main function
main() {
  # Parse command line arguments
  local command="scan"
  if [ $# -gt 0 ]; then
    command="$1"
  fi
  
  # Initialize database
  init_database
  
  case "$command" in
    scan)
      log "Processing movies in $INPUT_DIR"
      
      # Check if input directory exists and has proper permissions
      if [ ! -d "$INPUT_DIR" ]; then
        log "ERROR: Input directory $INPUT_DIR does not exist"
        exit 1
      fi
      
      if [ ! -r "$INPUT_DIR" ]; then
        log "ERROR: Cannot read from input directory $INPUT_DIR"
        exit 1
      fi
      
      # Check if output directory is writable
      if [ ! -w "$OUTPUT_DIR" ]; then
        log "ERROR: Cannot write to output directory $OUTPUT_DIR"
        exit 1
      fi
      
      # Check disk space on output directory
      local free_space=$(df -m "$OUTPUT_DIR" | awk 'NR==2 {print $4}')
      log "Free space in output directory: $free_space MB"
      if [ "$free_space" -lt 100 ]; then
        log "WARNING: Low disk space on output directory ($free_space MB)"
      fi
      
      # List available movie files
      log "Available movie files:"
      # Create a list to store extensions in lowercase
      local extensions_list=()
      for ext in $FILE_EXTENSIONS; do
        extensions_list+=("$(echo "$ext" | tr '[:upper:]' '[:lower:]')")
      done
      
      # Build find command with all extensions
      local find_cmd="find \"$INPUT_DIR\" -type f "
      for ext in "${extensions_list[@]}"; do
        find_cmd+=" -o -iname \"*.${ext}\""
      done
      
      # Remove the first " -o " that was added
      find_cmd=${find_cmd/ -o / }
      
      # Execute the find command
      eval $find_cmd | sort | while read file; do
        log " - $(basename "$file")"
      done
      
      process_in_parallel
      ;;
      
    verify)
      log "Verifying thumbnails (dry run mode)"
      verify_thumbnails "dry-run"
      ;;
      
    delete-missing)
      log "WARNING: Deleting movies with missing thumbnails"
      verify_thumbnails "delete"
      ;;
      
    stats)
      print_stats
      ;;
      
    cleanup)
      log "Running complete cleanup"
      cleanup_everything
      ;;
      
    help|--help|-h)
      display_help
      exit 0
      ;;
      
    *)
      log "ERROR: Unknown command: $command"
      display_help
      exit 1
      ;;
  esac
  
  log "All processing complete"
}

# Trap errors
trap 'log "ERROR: Script failed at line $LINENO"' ERR

# Run main function with all arguments
main "$@"
log "Script finished successfully!"
