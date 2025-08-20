-- Configuration file for Movie Thumbnailer MPV scripts
-- Place this file in the same directory as the Lua scripts
-- Or modify the scripts directly with your settings

return {
    -- Movie Thumbnailer API Configuration
    host = "localhost",
    port = "8080",
    
    -- OSD Message Settings
    archive_message_duration = 3,  -- seconds
    delete_message_duration = 3,   -- seconds
    error_message_duration = 4,    -- seconds
    
    -- Behavior Settings
    auto_skip_on_success = true,   -- Skip to next video after successful operation
    show_verbose_logs = false,     -- Show detailed logging in MPV console
    
    -- API Timeout (curl timeout in seconds)
    api_timeout = 10,
    
    -- Custom Icons (can be changed to your preference)
    icons = {
        archive = "📦",
        delete = "🗑️", 
        success = "✅",
        error = "❌"
    }
}
