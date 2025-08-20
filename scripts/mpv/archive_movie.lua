-- Archive current movie via Movie Thumbnailer API
-- Place this file in ~/.config/mpv/scripts/ or your mpv scripts directory
-- Add to input.conf: key_name script-message archive-movie

local msg = require 'mp.msg'
local utils = require 'mp.utils'

-- Configuration
local THUMBNAILER_HOST = "localhost"
local THUMBNAILER_PORT = "8080"
local API_ENDPOINT = string.format("http://%s:%s/api/v1/video/archive", THUMBNAILER_HOST, THUMBNAILER_PORT)

-- Function to get filename from full path
local function get_filename(path)
    if not path then
        return nil
    end
    return path:match("([^/\\]+)$")
end

-- Function to make HTTP POST request
local function make_api_request(filename, callback)
    local json_data = string.format('{"filename": "%s"}', filename)
    
    local args = {
        "curl",
        "-s", -- Silent
        "-X", "POST",
        "-H", "Content-Type: application/json",
        "-d", json_data,
        API_ENDPOINT
    }
    
    msg.info("Making API request to archive: " .. filename)
    
    local res = utils.subprocess({
        args = args,
        cancellable = false,
    })
    
    if callback then
        callback(res, filename)
    end
end

-- Function to handle API response
local function handle_response(res, filename)
    if res.status == 0 then
        -- Parse JSON response (simple parsing for success field)
        local response_text = res.stdout or ""
        msg.verbose("API Response: " .. response_text)
        
        if response_text:match('"success"%s*:%s*true') then
            mp.osd_message("✅ Archived: " .. filename, 3)
            msg.info("Successfully archived: " .. filename)
            
            -- Skip to next file after successful archival
            mp.command("playlist-next")
        else
            -- Try to extract error message
            local error_msg = response_text:match('"error"%s*:%s*"([^"]*)"')
            if error_msg then
                mp.osd_message("❌ Archive failed: " .. error_msg, 4)
                msg.error("Archive failed for " .. filename .. ": " .. error_msg)
            else
                mp.osd_message("❌ Archive failed: Unknown error", 4)
                msg.error("Archive failed for " .. filename .. ": Unknown error")
            end
        end
    else
        mp.osd_message("❌ Archive failed: Connection error", 4)
        msg.error("Failed to connect to thumbnailer API for " .. filename)
        msg.error("curl exit code: " .. tostring(res.status))
        msg.error("stderr: " .. (res.stderr or ""))
    end
end

-- Main function to archive current movie
local function archive_current_movie()
    local path = mp.get_property("path")
    
    if not path then
        mp.osd_message("❌ No file currently playing", 2)
        msg.warn("No file currently playing")
        return
    end
    
    local filename = get_filename(path)
    
    if not filename then
        mp.osd_message("❌ Could not extract filename", 2)
        msg.error("Could not extract filename from path: " .. path)
        return
    end
    
    mp.osd_message("📦 Archiving: " .. filename .. "...", 2)
    msg.info("Archiving movie: " .. filename)
    
    -- Make the API request
    make_api_request(filename, handle_response)
end

-- Register the script message handler
mp.register_script_message("archive-movie", archive_current_movie)

-- Alternative: Register as a key binding directly (uncomment if preferred)
-- mp.add_key_binding("a", "archive-movie", archive_current_movie)

msg.info("Archive movie script loaded. Use 'script-message archive-movie' to archive current movie.")
