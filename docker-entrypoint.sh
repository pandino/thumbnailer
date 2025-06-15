#!/bin/sh

# Startup script for movie-thumbnailer
# This script runs the database migration before starting the main application

set -e

echo "Movie Thumbnailer starting..."

# Set default database path if not provided
if [ -z "$DATABASE_PATH" ]; then
    DATABASE_PATH="${DATA_DIR}/thumbnailer.db"
fi

echo "Database path: $DATABASE_PATH"

# Ensure data directory exists
mkdir -p "$(dirname "$DATABASE_PATH")"

# Find the migrate binary (look in current directory first, then /app)
MIGRATE_BIN="./migrate"
if [ ! -f "$MIGRATE_BIN" ]; then
    MIGRATE_BIN="/app/migrate"
fi

# Run database migration
echo "Running database migration..."
$MIGRATE_BIN -db "$DATABASE_PATH"

if [ $? -eq 0 ]; then
    echo "Migration completed successfully"
else
    echo "Migration failed, exiting"
    exit 1
fi

# Find the main application binary
MAIN_BIN="./movie-thumbnailer"
if [ ! -f "$MAIN_BIN" ]; then
    MAIN_BIN="/app/movie-thumbnailer"
fi

# Start the main application with all passed arguments
echo "Starting movie-thumbnailer..."
exec $MAIN_BIN "$@"
