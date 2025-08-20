# MPV Scripts for Movie Thumbnailer Integration

This directory contains Lua scripts for MPV player that integrate with the Movie Thumbnailer API to archive and delete videos directly from the player.

## Scripts

### Basic Scripts (using curl)
- `archive_movie.lua` - Archives the currently playing movie and skips to the next video
- `delete_movie.lua` - Marks the currently playing movie for deletion and skips to the next video

### Enhanced Scripts (using curl with configuration)
- `archive_movie_enhanced.lua` - Feature-rich version with config file support
- `delete_movie_enhanced.lua` - Feature-rich version with config file support

### Native HTTP Scripts (auto-detecting HTTP libraries)
- `archive_movie_native.lua` - Uses native Lua HTTP libraries when available, falls back to curl
- `delete_movie_native.lua` - Uses native Lua HTTP libraries when available, falls back to curl

**HTTP Library Detection Order:**
1. **LuaSocket** (most common) - `socket.http` + `ltn12`
2. **lua-http** (modern async) - `http.request`  
3. **curl subprocess** (fallback) - Always available

### Script Comparison

| Feature | Basic | Enhanced | Native |
|---------|-------|----------|--------|
| **HTTP Method** | curl only | curl only | Auto-detect → curl |
| **Configuration** | Hardcoded | config.lua | Hardcoded |
| **Error Handling** | Basic | Advanced | Advanced |
| **Performance** | Good | Good | Best (with native libs) |
| **Dependencies** | curl | curl | LuaSocket/lua-http preferred |
| **Best For** | Simple setup | Power users | Performance-focused |

**Recommendation**: Start with **Basic** scripts, upgrade to **Native** for better performance if you have Lua HTTP libraries installed.

## Installation

1. **Copy scripts to MPV scripts directory:**
   ```bash
   # Linux/macOS
   mkdir -p ~/.config/mpv/scripts/
   cp archive_movie.lua ~/.config/mpv/scripts/
   cp delete_movie.lua ~/.config/mpv/scripts/
   
   # Windows
   mkdir "%APPDATA%\mpv\scripts\"
   copy archive_movie.lua "%APPDATA%\mpv\scripts\"
   copy delete_movie.lua "%APPDATA%\mpv\scripts\"
   ```

2. **Configure key bindings in `~/.config/mpv/input.conf`:**
   ```
   # Archive current movie with 'a' key
   a script-message archive-movie
   
   # Delete current movie with 'd' key  
   d script-message delete-movie
   
   # Alternative key bindings (choose what works for you)
   # Shift+a script-message archive-movie
   # Shift+d script-message delete-movie
   # CTRL+a script-message archive-movie
   # CTRL+d script-message delete-movie
   ```

## Configuration

The scripts are configured to connect to the Movie Thumbnailer API at `localhost:8080` by default. To change this, edit the configuration section in each script:

```lua
-- Configuration
local THUMBNAILER_HOST = "localhost"  -- Change to your server IP/hostname
local THUMBNAILER_PORT = "8080"       -- Change to your server port
```

## Usage

1. **Start Movie Thumbnailer server** (make sure it's running on the configured host/port)

2. **Play videos in MPV** with a playlist or individual files

3. **Use keyboard shortcuts:**
   - Press `a` to archive the current movie
   - Press `d` to mark the current movie for deletion
   
4. **Visual feedback:**
   - Success messages appear as OSD overlays
   - Scripts automatically skip to the next video after successful operations
   - Error messages are displayed if the operation fails

## Features

### Archive Script (`archive_movie.lua`)
- ✅ Extracts filename from current playing file
- ✅ Calls `/api/v1/video/archive` endpoint
- ✅ Shows OSD confirmation messages
- ✅ Automatically skips to next video on success
- ✅ Handles API errors gracefully
- ✅ Logs operations for debugging

### Delete Script (`delete_movie.lua`)  
- ✅ Extracts filename from current playing file
- ✅ Calls `/api/v1/video/delete` endpoint
- ✅ Shows OSD confirmation messages
- ✅ Automatically skips to next video on success
- ✅ Handles API errors gracefully  
- ✅ Logs operations for debugging

## Requirements

- **MPV Player** with Lua scripting support
- **HTTP Library** (recommended but not required):
  - **LuaSocket** (`luarocks install luasocket`) - Most efficient
  - **lua-http** (`luarocks install http`) - Modern alternative  
- **curl** command-line tool (fallback when native libraries unavailable)
- **Movie Thumbnailer server** running and accessible
- Video files must be in the Movie Thumbnailer database (already scanned)

## Troubleshooting

### Script not working
- Check MPV console (`` ` `` key) for error messages
- Verify scripts are in the correct directory
- Ensure key bindings are properly configured in `input.conf`
- For native HTTP scripts: Check which HTTP method is being used in MPV console

### API connection errors  
- Verify Movie Thumbnailer server is running
- Check host/port configuration in scripts
- Test API manually: `curl -X POST -H "Content-Type: application/json" -d '{"filename":"test.mp4"}' http://localhost:8080/api/v1/video/archive`
- For LuaSocket users: Ensure `ltn12` is also installed (`luarocks install luasocket` includes it)

### Performance comparison
- **LuaSocket**: Fastest, most efficient for simple requests
- **lua-http**: Modern, supports HTTP/2, good for complex scenarios
- **curl subprocess**: Slower due to process overhead, but universally available

### Files not found in database
- Ensure videos have been scanned by Movie Thumbnailer
- Check that filenames match exactly (case-sensitive)
- Videos must have thumbnails generated to appear in database

## Alternative Key Bindings

You can customize the key bindings in `input.conf`. Some suggestions:

```
# Function keys
F1 script-message archive-movie
F2 script-message delete-movie

# Number pad
KP1 script-message archive-movie  
KP2 script-message delete-movie

# Letter combinations
ALT+a script-message archive-movie
ALT+d script-message delete-movie

# Special keys
DEL script-message delete-movie
END script-message archive-movie
```

## Integration Workflow

1. **Video Management Workflow:**
   - Watch videos in MPV with playlist
   - Press `a` to archive good content
   - Press `d` to mark unwanted content for deletion
   - Videos are automatically processed in the background
   - Use Movie Thumbnailer web interface for bulk operations

2. **Batch Processing:**
   - Archive/delete videos during playback
   - Use Movie Thumbnailer control page to process queues
   - Monitor operations via web interface statistics

This integration provides a seamless workflow for managing large video collections directly from the media player.
