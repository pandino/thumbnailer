-- Delete current movie via Movie Thumbnailer API
-- Place this file in ~/.config/mpv/scripts/ or your mpv scripts directory
-- Add to input.conf: key_name script-message delete-movie

local msg = require 'mp.msg'
local utils = require 'mp.utils'

-- Configuration
local THUMBNAILER_HOST = "localhost"
local THUMBNAILER_PORT = "8080"
local API_ENDPOINT = string.format("http://%s:%s/api/v1/video/delete", THUMBNAILER_HOST, THUMBNAILER_PORT)

-- Double-press confirmation state
local pending_deletion = false
local pending_filename = nil
local confirmation_timer = nil
local CONFIRMATION_TIMEOUT = 3 -- seconds

-- Function to get filename from full path
local function get_filename(path)
    if not path then
        return nil
    end
    return path:match("([^/\\]+)$")
end

-- Function to reset confirmation state
local function reset_confirmation_state()
    pending_deletion = false
    pending_filename = nil
    if confirmation_timer then
        confirmation_timer:kill()
        confirmation_timer = nil
    end
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
    
    msg.info("Making API request to delete: " .. filename)
    
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
            mp.osd_message("🗑️ Marked for deletion: " .. filename, 3)
            msg.info("Successfully marked for deletion: " .. filename)
            
            -- Skip to next file after successful deletion marking
            mp.command("playlist-next")
        else
            -- Try to extract error message
            local error_msg = response_text:match('"error"%s*:%s*"([^"]*)"')
            if error_msg then
                mp.osd_message("❌ Delete failed: " .. error_msg, 4)
                msg.error("Delete failed for " .. filename .. ": " .. error_msg)
            else
                mp.osd_message("❌ Delete failed: Unknown error", 4)
                msg.error("Delete failed for " .. filename .. ": Unknown error")
            end
        end
    else
        mp.osd_message("❌ Delete failed: Connection error", 4)
        msg.error("Failed to connect to thumbnailer API for " .. filename)
        msg.error("curl exit code: " .. tostring(res.status))
        msg.error("stderr: " .. (res.stderr or ""))
    end
end

-- Function to execute the actual deletion
local function execute_deletion(filename)
    reset_confirmation_state()
    mp.osd_message("🗑️ Marking for deletion: " .. filename .. "...", 2)
    msg.info("Marking movie for deletion: " .. filename)
    
    -- Make the API request
    make_api_request(filename, handle_response)
end

-- Main function to delete current movie (with double-press confirmation)
local function delete_current_movie()
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
    
    -- Check if we're already waiting for confirmation
    if pending_deletion then
        if pending_filename == filename then
            -- Second press for same file - execute deletion
            msg.info("Deletion confirmed for: " .. filename)
            execute_deletion(filename)
        else
            -- Second press for different file - start new confirmation
            reset_confirmation_state()
            mp.osd_message("⚠️ Press 'd' again within " .. CONFIRMATION_TIMEOUT .. "s to delete: " .. filename, CONFIRMATION_TIMEOUT)
            msg.info("Deletion confirmation requested for: " .. filename)
            
            pending_deletion = true
            pending_filename = filename
            
            -- Set up timeout timer
            confirmation_timer = mp.add_timeout(CONFIRMATION_TIMEOUT, function()
                if pending_deletion and pending_filename == filename then
                    mp.osd_message("❌ Deletion cancelled (timeout)", 2)
                    msg.info("Deletion confirmation timed out for: " .. filename)
                    reset_confirmation_state()
                end
            end)
        end
    else
        -- First press - show confirmation message
        mp.osd_message("⚠️ Press 'd' again within " .. CONFIRMATION_TIMEOUT .. "s to delete: " .. filename, CONFIRMATION_TIMEOUT)
        msg.info("Deletion confirmation requested for: " .. filename)
        
        pending_deletion = true
        pending_filename = filename
        
        -- Set up timeout timer
        confirmation_timer = mp.add_timeout(CONFIRMATION_TIMEOUT, function()
            if pending_deletion and pending_filename == filename then
                mp.osd_message("❌ Deletion cancelled (timeout)", 2)
                msg.info("Deletion confirmation timed out for: " .. filename)
                reset_confirmation_state()
            end
        end)
    end
end

-- Register the script message handler
mp.register_script_message("delete-movie", delete_current_movie)

-- Alternative: Register as a key binding directly (uncomment if preferred)
-- mp.add_key_binding("d", "delete-movie", delete_current_movie)

msg.info("Delete movie script loaded. Press 'd' twice within 3 seconds to mark current movie for deletion.")
