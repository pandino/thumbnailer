-- Show the preference model's prediction for the current movie via Movie Thumbnailer API
-- Place this file in ~/.config/mpv/scripts/ or your mpv scripts directory
-- Shown automatically when a file starts; add to input.conf to show it again: key_name script-message show-prediction

local msg = require 'mp.msg'
local utils = require 'mp.utils'

-- Configuration
local THUMBNAILER_HOST = "localhost"
local THUMBNAILER_PORT = "8080"
local OSD_DURATION = 4 -- seconds
local API_ENDPOINT = string.format("http://%s:%s/api/v1/video/status/", THUMBNAILER_HOST, THUMBNAILER_PORT)

-- Function to get filename from full path
local function get_filename(path)
    if not path then
        return nil
    end
    return path:match("([^/\\]+)$")
end

-- Function to percent-encode a filename for the URL path
local function url_encode(s)
    return (s:gsub("[^%w%-%._~]", function(c)
        return string.format("%%%02X", string.byte(c))
    end))
end

-- Function to fetch and show the prediction, without blocking playback
local function show_prediction()
    local filename = get_filename(mp.get_property("path"))
    if not filename then
        return
    end

    mp.command_native_async({
        name = "subprocess",
        args = { "curl", "-s", "-m", "5", API_ENDPOINT .. url_encode(filename) },
        capture_stdout = true,
        playback_only = false,
    }, function(success, res)
        -- Ignore answers for a file that is no longer playing
        if get_filename(mp.get_property("path")) ~= filename then
            return
        end
        if not success or res.status ~= 0 then
            msg.warn("Could not reach the thumbnailer for: " .. filename)
            return
        end

        local response = utils.parse_json(res.stdout or "")
        if not response or not response.success then
            msg.info("Movie not in the thumbnailer database: " .. filename)
            return
        end
        if not response.prediction then
            mp.osd_message("🤖 Not scored yet", OSD_DURATION)
            return
        end

        mp.osd_message("🤖 " .. response.prediction, OSD_DURATION)
        msg.info("Prediction for " .. filename .. ": " .. response.prediction)
    end)
end

mp.register_event("file-loaded", show_prediction)
mp.register_script_message("show-prediction", show_prediction)

msg.info("Show prediction script loaded. Use 'script-message show-prediction' to show it again.")
