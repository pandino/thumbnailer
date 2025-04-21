#!/bin/bash
#
# Thumbnail Viewer - View thumbnails created by movie-thumbnailer.sh
# Works with feh to view and mark thumbnails as viewed
#

# Configuration (can be overridden with environment variables)
THUMBNAIL_DIR="${THUMBNAIL_OUTPUT_DIR:-/thumbnails}"
DATA_DIR="${DATA_DIR:-/data}"
DB_FILE="${DB_FILE:-$DATA_DIR/thumbnailer.db}"
FEH_OPTIONS="${FEH_OPTIONS:---borderless --scale-down --auto-zoom --draw-filename --hide-pointer}"
DEBUG="${DEBUG:-false}"

# Function to log messages
log() {
  local message="$1"
  local pid_prefix="[PID:$$] "

  # Format the message with timestamp and PID
  local formatted_message="[$(date '+%Y-%m-%d %H:%M:%S')] ${pid_prefix}$message"

  # Only write to log file if in debug mode
  if [ "$DEBUG" = "true" ]; then
    echo "$formatted_message" >> "$THUMBNAIL_DIR/viewer.log"
  fi

  # Always display to stdout
  echo "$formatted_message"
}

# Setup
mkdir -p "$DATA_DIR"

# Only create log file if in debug mode
if [ "$DEBUG" = "true" ]; then
  touch "$THUMBNAIL_DIR/viewer.log"
  log "Script started with PID $$"
fi

# Check for required tools
for tool in feh sqlite3; do
  if ! command -v $tool &>/dev/null; then
    log "ERROR: Required tool '$tool' is not installed"
    exit 1
  else
    log "Found required tool: $tool at $(which $tool)"
  fi
done

# Check if database exists
if [ ! -f "$DB_FILE" ]; then
  log "ERROR: Database file $DB_FILE does not exist"
  echo "Please run the movie-thumbnailer.sh script first to create thumbnails."
  exit 1
fi

# Update database schema to include 'viewed' status if it doesn't exist
update_schema() {
  log "Checking database schema..."
  
  # Check if 'viewed' column exists in thumbnails table
  local column_exists=$(sqlite3 "$DB_FILE" "PRAGMA table_info(thumbnails);" | grep -c "viewed")
  
  if [ "$column_exists" -eq 0 ]; then
    log "Adding 'viewed' column to thumbnails table"
    sqlite3 "$DB_FILE" "ALTER TABLE thumbnails ADD COLUMN viewed INTEGER DEFAULT 0;"
    
    if [ $? -eq 0 ]; then
      log "Successfully updated database schema"
    else
      log "ERROR: Failed to update database schema"
      exit 1
    fi
  else
    log "Database schema already up to date"
  fi
}

# Function to mark a thumbnail as viewed
mark_as_viewed() {
  local thumbnail_path="$1"
  
  # Get the corresponding movie path
  local movie_path=$(sqlite3 "$DB_FILE" "SELECT movie_path FROM thumbnails WHERE thumbnail_path = '$thumbnail_path';")
  
  if [ -z "$movie_path" ]; then
    log "ERROR: Could not find movie for thumbnail: $thumbnail_path"
    return 1
  fi
  
  log "Marking as viewed: $(basename "$movie_path")"
  
  # Update the database
  sqlite3 "$DB_FILE" "UPDATE thumbnails SET viewed = 1, status = 'viewed' WHERE movie_path = '$movie_path';"
  
  if [ $? -eq 0 ]; then
    log "Successfully marked as viewed: $(basename "$movie_path")"
    return 0
  else
    log "ERROR: Failed to mark as viewed: $(basename "$movie_path")"
    return 1
  fi
}

# Function to create a temporary file with the list of unviewed thumbnails
create_unviewed_list() {
  local temp_file=$(mktemp)
  
  log "Creating list of unviewed thumbnails..."
  
  # Get all successful thumbnails that have not been viewed
  local query="SELECT thumbnail_path FROM thumbnails WHERE status = 'success' AND (viewed = 0 OR viewed IS NULL) AND thumbnail_path IS NOT NULL ORDER BY created_at DESC;"
  
  # Execute the query and write results to temp file
  sqlite3 "$DB_FILE" "$query" > "$temp_file"
  
  # Count the number of thumbnails
  local count=$(wc -l < "$temp_file")
  
  # Check if there are any thumbnails to view
  if [ "$count" -eq 0 ]; then
    log "No unviewed thumbnails found"
    rm -f "$temp_file"
    return 1
  fi
  
  log "Found $count unviewed thumbnails"
  echo "$temp_file"
  return 0
}

# Function to create the mark-as-viewed action script for feh
create_action_script() {
  local action_script="$DATA_DIR/feh_action.sh"
  
  log "Creating feh action script at $action_script"
  
  # Create the script
  cat > "$action_script" << 'EOF'
#!/bin/bash

# Get the current file from feh
current_file="$1"

# Get the directory of this script
SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"

# Call the main script to mark as viewed
"$SCRIPT_DIR/thumbnail-viewer.sh" mark-viewed "$current_file"

# Signal feh to go to the next image
exit 0
EOF
  
  # Make it executable
  chmod +x "$action_script"
  
  if [ -f "$action_script" ] && [ -x "$action_script" ]; then
    log "Successfully created feh action script"
    return 0
  else
    log "ERROR: Failed to create feh action script"
    return 1
  fi
}

# Function to start feh with the unviewed thumbnails
start_feh() {
  log "Starting feh..."
  
  # Create the list of unviewed thumbnails
  local thumbnail_list=$(create_unviewed_list)
  
  if [ $? -ne 0 ]; then
    echo "No unviewed thumbnails found. Run the movie-thumbnailer.sh script to create thumbnails."
    return 1
  fi
  
  # Create the action script
  create_action_script
  
  if [ $? -ne 0 ]; then
    log "Failed to create action script"
    rm -f "$thumbnail_list"
    return 1
  fi
  
  # Configure feh key binding
  # 'm' key will execute the action script to mark as viewed
  local feh_keys="--action1 \"$DATA_DIR/feh_action.sh %F\""
  local feh_bind="--bind m:action1"
  
  # Start feh with the unviewed thumbnails
  log "Launching feh with options: $FEH_OPTIONS $feh_keys $feh_bind --filelist $thumbnail_list"
  
  # Use eval to handle the complex command with quotes properly
  eval feh $FEH_OPTIONS $feh_keys $feh_bind --filelist "$thumbnail_list"
  
  local exit_code=$?
  log "feh exited with code $exit_code"
  
  # Clean up
  rm -f "$thumbnail_list"
  
  return $exit_code
}

# Function to display usage
display_help() {
  echo "Thumbnail Viewer - View thumbnails created by movie-thumbnailer.sh"
  echo ""
  echo "Usage: $0 [command]"
  echo ""
  echo "Commands:"
  echo "  view             - View unviewed thumbnails (default)"
  echo "  mark-viewed PATH - Mark a specific thumbnail as viewed"
  echo "  reset-views      - Reset all viewed statuses to unviewed"
  echo "  list-unviewed    - List all unviewed thumbnails"
  echo "  list-viewed      - List all viewed thumbnails"
  echo "  help             - Show this help"
  echo ""
  echo "Environment Variables:"
  echo "  THUMBNAIL_OUTPUT_DIR - Directory to store generated thumbnails (default: /thumbnails)"
  echo "  DATA_DIR             - Directory for the database (default: /data)"
  echo "  FEH_OPTIONS          - Additional options for feh (default: --borderless --scale-down...)"
  echo "  DEBUG                - Enable debug output (default: false)"
  echo ""
  echo "feh Keybindings:"
  echo "  m                    - Mark current thumbnail as viewed and go to next image"
  echo ""
  echo "Examples:"
  echo "  $0                   - View all unviewed thumbnails"
  echo "  $0 reset-views       - Reset all viewed statuses to unviewed"
  echo ""
}

# Function to list unviewed thumbnails
list_unviewed() {
  log "Listing unviewed thumbnails..."
  
  # Get all successful thumbnails that have not been viewed
  local query="SELECT thumbnail_path, movie_path FROM thumbnails WHERE status = 'success' AND (viewed = 0 OR viewed IS NULL) AND thumbnail_path IS NOT NULL ORDER BY created_at DESC;"
  
  # Execute the query
  local results=$(sqlite3 -separator "|" "$DB_FILE" "$query")
  
  # Count the number of thumbnails
  local count=$(echo "$results" | wc -l)
  
  # Check if there are any thumbnails
  if [ "$count" -eq 0 ] || [ -z "$results" ]; then
    echo "No unviewed thumbnails found"
    return 1
  fi
  
  echo "Found $count unviewed thumbnails:"
  echo "------------------------------"
  
  # Display the results
  echo "$results" | while IFS="|" read -r thumbnail_path movie_path; do
    # Skip if empty
    [ -z "$thumbnail_path" ] && continue
    
    echo "Movie: $(basename "$movie_path")"
    echo "Thumbnail: $thumbnail_path"
    echo "------------------------------"
  done
  
  return 0
}

# Function to list viewed thumbnails
list_viewed() {
  log "Listing viewed thumbnails..."
  
  # Get all thumbnails that have been viewed
  local query="SELECT thumbnail_path, movie_path FROM thumbnails WHERE viewed = 1 AND thumbnail_path IS NOT NULL ORDER BY created_at DESC;"
  
  # Execute the query
  local results=$(sqlite3 -separator "|" "$DB_FILE" "$query")
  
  # Count the number of thumbnails
  local count=$(echo "$results" | wc -l)
  
  # Check if there are any thumbnails
  if [ "$count" -eq 0 ] || [ -z "$results" ]; then
    echo "No viewed thumbnails found"
    return 1
  fi
  
  echo "Found $count viewed thumbnails:"
  echo "------------------------------"
  
  # Display the results
  echo "$results" | while IFS="|" read -r thumbnail_path movie_path; do
    # Skip if empty
    [ -z "$thumbnail_path" ] && continue
    
    echo "Movie: $(basename "$movie_path")"
    echo "Thumbnail: $thumbnail_path"
    echo "------------------------------"
  done
  
  return 0
}

# Function to reset all viewed statuses
reset_views() {
  log "Resetting all viewed statuses..."
  
  # Update the database
  sqlite3 "$DB_FILE" "UPDATE thumbnails SET viewed = 0 WHERE viewed = 1;"
  
  if [ $? -eq 0 ]; then
    local count=$(sqlite3 "$DB_FILE" "SELECT changes();")
    log "Successfully reset viewed status for $count thumbnails"
    echo "Reset viewed status for $count thumbnails"
    return 0
  else
    log "ERROR: Failed to reset viewed statuses"
    echo "Failed to reset viewed statuses"
    return 1
  fi
}

# Main function
main() {
  # Parse command line arguments
  local command="view"
  local arg=""
  
  if [ $# -gt 0 ]; then
    command="$1"
    # If there's a second argument, store it
    if [ $# -gt 1 ]; then
      arg="$2"
    fi
  fi
  
  # Check database and update schema
  update_schema
  
  case "$command" in
    view)
      start_feh
      ;;
    
    mark-viewed)
      if [ -z "$arg" ]; then
        log "ERROR: No thumbnail path provided for mark-viewed command"
        echo "Usage: $0 mark-viewed PATH"
        exit 1
      fi
      mark_as_viewed "$arg"
      ;;
    
    reset-views)
      reset_views
      ;;
    
    list-unviewed)
      list_unviewed
      ;;
    
    list-viewed)
      list_viewed
      ;;
    
    help | --help | -h)
      display_help
      exit 0
      ;;
    
    *)
      log "ERROR: Unknown command: $command"
      display_help
      exit 1
      ;;
  esac
  
  log "Command '$command' completed"
}

# Run main function with all arguments
main "$@"
