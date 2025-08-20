-- Archive current movie via Movie Thumbnailer API (Enhanced Version)
-- Place this file in ~/.config/mpv/scripts/ or your mpv scripts directory
-- Add to input.conf: key_name script-message archive-movie

local msg = require 'mp.msg'
local utils = require 'mp.utils'

-- Try to load configuration file, fallback to defaults
local config
local config_path = mp.get_script_directory() .. "/config.lua"
local config_file = io.open(config_path, "r")

if config_file then
    config_file:close()
    config = dofile(config_path)
    msg.info("Loaded configuration from: " .. config_path)
else
    -- Default configuration
    config = {
        host = "localhost",
        port = "8080",
        archive_message_duration = 3,
        error_message_duration = 4,
        auto_skip_on_success = true,
        show_verbose_logs = false,
        api_timeout = 10,
        icons = {
            archive = "📦",
            success = "✅",
            error = "❌"
        }
    }
    msg.info("Using default configuration (config.lua not found)")
end

local API_ENDPOINT = string.format("http://%s:%s/api/v1/video/archive", config.host, config.port)

-- Function to get filename from full path
local function get_filename(path)
    if not path then
        return nil
    end
    return path:match("([^/\\]+)$")
end

-- Function to make HTTP POST request with timeout
local function make_api_request(filename, callback)
    local json_data = string.format('{"filename": "%s"}', filename:gsub('"', '\\"'))
    
    local args = {
        "curl",
        "-s", -- Silent
        "-m", tostring(config.api_timeout), -- Timeout
        "-X", "POST",
        "-H", "Content-Type: application/json",
        "-d", json_data,
        API_ENDPOINT
    }
    
    msg.info("Making API request to archive: " .. filename)
    if config.show_verbose_logs then
        msg.verbose("API Endpoint: " .. API_ENDPOINT)
        msg.verbose("Request payload: " .. json_data)
    end
    
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
    local success_icon = config.icons.success
    local error_icon = config.icons.error
    local archive_icon = config.icons.archive
    
    if res.status == 0 then
        -- Parse JSON response (simple parsing for success field)
        local response_text = res.stdout or ""
        
        if config.show_verbose_logs then
            msg.verbose("API Response: " .. response_text)
        end
        
        if response_text:match('"success"%s*:%s*true') then
            mp.osd_message(success_icon .. " Archived: " .. filename, config.archive_message_duration)
            msg.info("Successfully archived: " .. filename)
            
            -- Skip to next file after successful archival
            if config.auto_skip_on_success then
                mp.command("playlist-next")
            end
        else
            -- Try to extract error message
            local error_msg = response_text:match('"error"%s*:%s*"([^"]*)"')
            if error_msg then
                mp.osd_message(error_icon .. " Archive failed: " .. error_msg, config.error_message_duration)
                msg.error("Archive failed for " .. filename .. ": " .. error_msg)
            else
                mp.osd_message(error_icon .. " Archive failed: Unknown error", config.error_message_duration)
                msg.error("Archive failed for " .. filename .. ": Unknown error")
            end
            
            if config.show_verbose_logs then
                msg.verbose("Full API response: " .. response_text)
            end
        end
    else
        local error_msg = "Connection error"
        if res.status == 28 then
            error_msg = "Timeout error"
        elseif res.status == 7 then
            error_msg = "Connection refused"
        end
        
        mp.osd_message(error_icon .. " Archive failed: " .. error_msg, config.error_message_duration)
        msg.error("Failed to connect to thumbnailer API for " .. filename)
        msg.error("curl exit code: " .. tostring(res.status))
        
        if res.stderr and res.stderr ~= "" then
            msg.error("curl stderr: " .. res.stderr)
        end
    end
end

-- Main function to archive current movie
local function archive_current_movie()
    local path = mp.get_property("path")
    
    if not path then
        mp.osd_message(config.icons.error .. " No file currently playing", 2)
        msg.warn("No file currently playing")
        return
    end
    
    local filename = get_filename(path)
    
    if not filename then
        mp.osd_message(config.icons.error .. " Could not extract filename", 2)
        msg.error("Could not extract filename from path: " .. path)
        return
    end
    
    mp.osd_message(config.icons.archive .. " Archiving: " .. filename .. "...", 2)
    msg.info("Archiving movie: " .. filename)
    
    -- Make the API request
    make_api_request(filename, handle_response)
end

-- Register the script message handler
mp.register_script_message("archive-movie", archive_current_movie)

-- Alternative: Register as a key binding directly (uncomment if preferred)
-- mp.add_key_binding("a", "archive-movie", archive_current_movie)

msg.info("Enhanced archive movie script loaded. Endpoint: " .. API_ENDPOINT)
