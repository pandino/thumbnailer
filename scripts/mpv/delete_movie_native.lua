-- Delete current movie via Movie Thumbnailer API (Native HTTP Version)
-- This version attempts to use Lua's native HTTP capabilities when available
-- Place this file in ~/.config/mpv/scripts/ or your mpv scripts directory
-- Add to input.conf: key_name script-message delete-movie

local msg = require 'mp.msg'
local utils = require 'mp.utils'

-- Try to load HTTP libraries (in order of preference)
local http_client = nil
local http_method = "none"

-- Try LuaSocket first (most common)
local success, http = pcall(require, 'socket.http')
if success and http then
    http_client = http
    http_method = "luasocket"
    msg.info("Using LuaSocket for HTTP requests")
else
    -- Try lua-http library
    local success_http, http_request = pcall(require, 'http.request')
    if success_http and http_request then
        http_client = http_request
        http_method = "lua-http"
        msg.info("Using lua-http library for HTTP requests")
    else
        -- Fallback to curl via subprocess
        http_method = "curl"
        msg.info("Using curl subprocess for HTTP requests (fallback)")
    end
end

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

-- Function to make HTTP POST request using LuaSocket
local function make_request_luasocket(filename, callback)
    local json_data = string.format('{"filename": "%s"}', filename:gsub('"', '\\"'))
    
    local body, status_code = http_client.request{
        url = API_ENDPOINT,
        method = "POST",
        headers = {
            ["Content-Type"] = "application/json",
            ["Content-Length"] = tostring(#json_data)
        },
        source = ltn12.source.string(json_data)
    }
    
    local response = {
        status = status_code == 200 and 0 or 1,
        stdout = body or "",
        stderr = status_code ~= 200 and ("HTTP " .. tostring(status_code)) or ""
    }
    
    if callback then
        callback(response, filename)
    end
end

-- Function to make HTTP POST request using lua-http
local function make_request_lua_http(filename, callback)
    local json_data = string.format('{"filename": "%s"}', filename:gsub('"', '\\"'))
    
    local request = http_client.new_from_uri(API_ENDPOINT)
    request.headers:upsert(":method", "POST")
    request.headers:upsert("content-type", "application/json")
    request:set_body(json_data)
    
    local headers, stream = request:go()
    local body = stream:get_body_as_string()
    
    local response = {
        status = (headers:get(":status") == "200") and 0 or 1,
        stdout = body or "",
        stderr = headers:get(":status") ~= "200" and ("HTTP " .. headers:get(":status")) or ""
    }
    
    if callback then
        callback(response, filename)
    end
end

-- Function to make HTTP POST request using curl (fallback)
local function make_request_curl(filename, callback)
    local json_data = string.format('{"filename": "%s"}', filename:gsub('"', '\\"'))
    
    local args = {
        "curl",
        "-s", -- Silent
        "-X", "POST",
        "-H", "Content-Type: application/json",
        "-d", json_data,
        API_ENDPOINT
    }
    
    local res = utils.subprocess({
        args = args,
        cancellable = false,
    })
    
    if callback then
        callback(res, filename)
    end
end

-- Universal HTTP request function
local function make_api_request(filename, callback)
    msg.info("Making API request to delete: " .. filename .. " (using " .. http_method .. ")")
    
    if http_method == "luasocket" then
        -- Need to load ltn12 for LuaSocket
        local ltn12_success, ltn12 = pcall(require, 'ltn12')
        if ltn12_success then
            _G.ltn12 = ltn12  -- Make it globally available
            make_request_luasocket(filename, callback)
        else
            msg.warn("LuaSocket detected but ltn12 not available, falling back to curl")
            make_request_curl(filename, callback)
        end
    elseif http_method == "lua-http" then
        make_request_lua_http(filename, callback)
    else
        make_request_curl(filename, callback)
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
        msg.error("Error details: " .. (res.stderr or ""))
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

msg.info("Native HTTP delete script loaded (" .. http_method .. "). Press 'd' twice within 3 seconds to mark current movie for deletion.")
