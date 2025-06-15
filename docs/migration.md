# Database Migration

This document explains the database migration utility that was added to handle the addition of the `file_size` column to the thumbnails table.

## Migration Utility

The migration utility (`cmd/migrate/main.go`) is a standalone tool that:

1. **Adds the `file_size` column** to existing databases that don't have it
2. **Populates file sizes** for existing movie records by scanning the file system
3. **Marks missing files** as deleted if the movie files no longer exist

### How it works

1. **Schema Migration**: Checks if the `file_size` column exists in the `thumbnails` table and adds it if missing
2. **Data Population**: Scans all existing thumbnail records and updates their file sizes by checking the actual movie files
3. **Cleanup**: Marks records as deleted if their corresponding movie files are missing

### Usage

```bash
# Run migration on a specific database
./migrate -db /path/to/thumbnails.db

# Show help
./migrate -h
```

### Docker Integration

The migration utility is automatically included in the Docker image and runs before the main application starts:

1. **Startup Script**: `docker-entrypoint.sh` runs the migration before starting the main application
2. **Automatic Migration**: No manual intervention required - migrations run automatically on container startup
3. **Safe Operation**: The migration is idempotent and can be run multiple times safely

### Migration Process

When the container starts:

```
Movie Thumbnailer starting...
Database path: /data/thumbnails.db
Running database migration...
2025/06/11 20:59:27 Starting database migration for: /data/thumbnails.db
2025/06/11 20:59:27 Adding file_size column to thumbnails table...
2025/06/11 20:59:27 file_size column added successfully
2025/06/11 20:59:27 Starting file size population...
2025/06/11 20:59:27 Processing 150 thumbnails...
2025/06/11 20:59:27 Migration summary:
2025/06/11 20:59:27   - Total records processed: 150
2025/06/11 20:59:27   - Updated file sizes: 142
2025/06/11 20:59:27   - Missing files (marked as deleted): 8
2025/06/11 20:59:27   - Errors: 0
2025/06/11 20:59:27 Migration completed successfully
Starting movie-thumbnailer...
```

### Benefits

- **Zero Downtime**: Existing installations upgrade automatically without data loss
- **Clean Separation**: Migration logic is separate from main application logic
- **Comprehensive**: Handles both schema changes and data migration
- **Safe**: Idempotent operations that can be run multiple times
- **Informative**: Detailed logging of migration progress and results

### Database Changes

The migration adds a single new column:

```sql
ALTER TABLE thumbnails ADD COLUMN file_size INTEGER DEFAULT 0;
```

For new installations, the column is included in the initial schema, so no migration is needed.
