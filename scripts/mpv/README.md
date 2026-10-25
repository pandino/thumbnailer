# MPV Scripts for Movie Thumbnailer Integration

This directory contains Lua scripts for MPV player that integrate with the Movie Thumbnailer API to archive and delete videos directly from the player.

## Scripts

### Basic Scripts (using curl)
- `archive_movie.lua` - Archives the currently playing movie and keeps playing it
- `delete_movie.lua` - Marks the currently playing movie for deletion (single press) and skips to the next video
- `show_prediction.lua` - Shows the preference model's prediction for each movie when it starts (e.g. "🤖 Delete 87%")

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

**Install only one variant of each script.** All variants register the same `archive-movie` / `delete-movie` messages, so with two installed a single key press sends the request twice and skips two videos.

**Quick install:** `./install.sh` copies the basic scripts and `config.lua` to `~/.config/mpv/scripts/`, asks whether to also add the enhanced and native variants (answer no to keep a single variant, see above), appends the `a`/`d` bindings to `input.conf` (backing it up first) and writes the host/port you enter into `config.lua`. Only the enhanced scripts read `config.lua`, so with the basic or native scripts edit the host/port in the scripts themselves (see [Configuration](#configuration)).

**Manual install:**

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
   
   # See input.conf.example for a ready-made version
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

The enhanced scripts instead read `config.lua` (host, port, OSD durations, auto-skip, verbose logging, curl timeout, icons) from `mp.get_script_directory()`, falling back to built-in defaults when it isn't there. mpv only sets that directory for scripts installed as a folder (e.g. `scripts/thumbnailer/main.lua`), not for single `.lua` files dropped into `scripts/`, so check the MPV console for `Loaded configuration from:` after installing them. Note also that mpv tries to load every `.lua` file in `scripts/` as a script, including `config.lua` itself.

## Usage

1. **Start Movie Thumbnailer server** (make sure it's running on the configured host/port)

2. **Play videos in MPV** with a playlist or individual files

3. **Use keyboard shortcuts:**
   - Press `a` to archive the current movie
   - Press `d` to mark the current movie for deletion (single press, no confirmation; the enhanced and native variants require a second press within 3 seconds)
   
4. **Visual feedback:**
   - Deleting shows "🗑️ Marked for deletion: filename  [N deleted this session]" and skips to the next video
   - Archiving shows "✅ Archived: filename" and keeps playing the current video (the enhanced and native variants skip to the next one)
   - Error messages are displayed if operations fail

## Model Prediction OSD (`show_prediction.lua`)

When a file starts playing, the script asks `GET /api/v1/video/status/{filename}` for the preference model's prediction and shows it on the OSD for 4 seconds, in the same form as the slideshow badge: the likelier final outcome and its probability, `Delete 87%` or `Archive 64%` (`Keep` for scores from before the archive model). Movies the model hasn't scored yet show `Not scored yet`; files that aren't in the database show nothing.

The request runs in the background, so playback never waits for the server. To show the prediction again, bind a key:

```
p script-message show-prediction
```

Install it like the other scripts and set `THUMBNAILER_HOST`/`THUMBNAILER_PORT` at the top. It needs `curl` and a server that includes `prediction` in the status response.

## Playlist Alias (`xxxmpv`)

To review movies already passed in the slideshow, this shell alias pulls them from the API, keeps the ones still on disk, shuffles them and plays 100 in MPV:

```bash
alias xxxmpv='curl -s "http://192.168.1.35:8080/api/thumbnails?status=success&viewed=1" | jq -r ".[].movie_filename" | while IFS= read -r f; do [ -f "/mnt/xxx/$f" ] && printf "/mnt/xxx/%s\n" "$f"; done | sort -R | head -n 100 | mpv --fullscreen --wayland-app-id=xxxmpv --input-ipc-server="$XDG_RUNTIME_DIR/xxxmpv.sock" --playlist=-'
```

- `status=success&viewed=1` selects thumbnailed movies that have been viewed (passed) in the slideshow
- `/mnt/xxx` is where the movies directory is mounted locally; adjust it, the host and the count (`head -n 100`) to taste
- Requires `curl` and `jq`; `--wayland-app-id` lets compositor window rules target this MPV instance
- `--input-ipc-server` lets global hotkeys drive this MPV instance whether or not it has focus, e.g. in Hyprland:

  ```lua
  local function xxxmpv(cmd)
      return hl.dsp.exec_cmd("echo '" .. cmd .. "' | socat - \"$XDG_RUNTIME_DIR/xxxmpv.sock\"")
  end
  hl.bind("CTRL + F14", xxxmpv("script-message archive-movie"))
  hl.bind("F15",        xxxmpv("script-message delete-movie"))
  ```

  Don't use Hyprland's `pass` dispatcher for this: it moves keyboard focus to MPV, and the focus switch re-reports the held key, so an unfocused MPV receives each key twice (a delete runs twice, and `CTRL + F14` also arrives as a bare `F14`).

## Features

### Archive Script (`archive_movie.lua`)
- ✅ Extracts filename from current playing file
- ✅ Calls `/api/v1/video/archive` endpoint
- ✅ Shows OSD confirmation messages
- ✅ Keeps playing the current video after archiving (uncomment `mp.command("playlist-next")` in `handle_response` to skip instead)
- ✅ Handles API errors gracefully
- ✅ Logs operations for debugging

### Delete Script (`delete_movie.lua`)  
- ✅ Extracts filename from current playing file
- ✅ **Single press**, no confirmation (the enhanced and native variants require pressing 'd' twice within 3 seconds)
- ✅ Calls `/api/v1/video/delete` endpoint (returns an error if the movie is already marked for deletion)
- ✅ Shows OSD messages, including a count of movies deleted in the current MPV session
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
- Native libraries must be installed for the Lua version mpv embeds (usually LuaJIT / Lua 5.1): `luarocks --lua-version 5.1 install luasocket`. Otherwise the scripts silently fall back to curl; the MPV console logs which method is in use

### Performance comparison
- **LuaSocket**: Fastest, most efficient for simple requests
- **lua-http**: Modern, supports HTTP/2, good for complex scenarios
- **curl subprocess**: Slower due to process overhead, but universally available

### Files not found in database
- Ensure videos have been scanned by Movie Thumbnailer
- Check that filenames match exactly (case-sensitive). Only the file's base name is sent, matched against `movie_filename`, so playing from a different mount point is fine
- Only the top level of the movies directory is scanned; files in subdirectories are never in the database
- Filenames containing `"` (basic/enhanced) or `\` (all variants) aren't escaped and produce an invalid JSON request

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
   - Pressing a key only queues the change: the files are actually archived/deleted by the next cleanup pass (every 6h inside the work window, or immediately with **Cleanup** on the control page). Deletions are never processed while `DISABLE_DELETION=true`
   - Each decision is also logged with the `player` stage as training data for the preference model

2. **Batch Processing:**
   - Archive/delete videos during playback
   - Use **Cleanup** on the Movie Thumbnailer control page to process the queues right away
   - Monitor operations via web interface statistics

This integration provides a seamless workflow for managing large video collections directly from the media player.
