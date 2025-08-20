#!/bin/bash

# Installation script for Movie Thumbnailer MPV integration
# This script installs the Lua scripts and sets up key bindings

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Detect OS and set MPV config directory
if [[ "$OSTYPE" == "linux-gnu"* ]] || [[ "$OSTYPE" == "darwin"* ]]; then
    MPV_CONFIG_DIR="$HOME/.config/mpv"
elif [[ "$OSTYPE" == "cygwin" ]] || [[ "$OSTYPE" == "msys" ]]; then
    MPV_CONFIG_DIR="$APPDATA/mpv"
else
    echo -e "${RED}Unsupported OS: $OSTYPE${NC}"
    exit 1
fi

MPV_SCRIPTS_DIR="$MPV_CONFIG_DIR/scripts"
INPUT_CONF="$MPV_CONFIG_DIR/input.conf"

echo -e "${BLUE}Movie Thumbnailer MPV Integration Installer${NC}"
echo "==========================================="
echo

# Create directories
echo -e "${YELLOW}Creating MPV config directories...${NC}"
mkdir -p "$MPV_SCRIPTS_DIR"

# Copy scripts
echo -e "${YELLOW}Installing Lua scripts...${NC}"
cp archive_movie.lua "$MPV_SCRIPTS_DIR/"
cp delete_movie.lua "$MPV_SCRIPTS_DIR/"
cp config.lua "$MPV_SCRIPTS_DIR/"

# Optional: Install enhanced versions
read -p "Install enhanced versions with configuration support? (y/n): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    cp archive_movie_enhanced.lua "$MPV_SCRIPTS_DIR/"
    cp delete_movie_enhanced.lua "$MPV_SCRIPTS_DIR/"
    echo -e "${GREEN}Enhanced versions installed${NC}"
fi

# Optional: Install native HTTP versions
read -p "Install native HTTP versions (auto-detect Lua HTTP libraries)? (y/n): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    cp archive_movie_native.lua "$MPV_SCRIPTS_DIR/"
    cp delete_movie_native.lua "$MPV_SCRIPTS_DIR/"
    echo -e "${GREEN}Native HTTP versions installed${NC}"
    echo -e "${BLUE}Note: Native versions will use LuaSocket or lua-http if available, otherwise fall back to curl${NC}"
fi

# Setup input.conf
echo -e "${YELLOW}Setting up key bindings...${NC}"

# Check if input.conf exists
if [ -f "$INPUT_CONF" ]; then
    echo -e "${YELLOW}Existing input.conf found. Backing up to input.conf.backup${NC}"
    cp "$INPUT_CONF" "$INPUT_CONF.backup"
else
    echo -e "${YELLOW}Creating new input.conf${NC}"
    touch "$INPUT_CONF"
fi

# Add key bindings if not already present
if ! grep -q "script-message archive-movie" "$INPUT_CONF"; then
    echo "" >> "$INPUT_CONF"
    echo "# Movie Thumbnailer Integration" >> "$INPUT_CONF"
    echo "a script-message archive-movie    # Archive current movie" >> "$INPUT_CONF"
    echo "d script-message delete-movie     # Mark current movie for deletion" >> "$INPUT_CONF"
    echo -e "${GREEN}Key bindings added to input.conf${NC}"
else
    echo -e "${YELLOW}Key bindings already exist in input.conf${NC}"
fi

# Configuration setup
echo
echo -e "${YELLOW}Configuration:${NC}"
echo "Default API endpoint: http://localhost:8080"
read -p "Enter Movie Thumbnailer host (press enter for localhost): " HOST
read -p "Enter Movie Thumbnailer port (press enter for 8080): " PORT

HOST=${HOST:-localhost}
PORT=${PORT:-8080}

# Update config.lua
if [ "$HOST" != "localhost" ] || [ "$PORT" != "8080" ]; then
    sed -i.bak "s/host = \"localhost\"/host = \"$HOST\"/" "$MPV_SCRIPTS_DIR/config.lua"
    sed -i.bak "s/port = \"8080\"/port = \"$PORT\"/" "$MPV_SCRIPTS_DIR/config.lua"
    echo -e "${GREEN}Configuration updated: $HOST:$PORT${NC}"
fi

# Show installation summary
echo
echo -e "${GREEN}Installation completed successfully!${NC}"
echo
echo "Files installed:"
echo "  📄 $MPV_SCRIPTS_DIR/archive_movie.lua"
echo "  📄 $MPV_SCRIPTS_DIR/delete_movie.lua"
echo "  📄 $MPV_SCRIPTS_DIR/config.lua"
echo "  📄 $INPUT_CONF (key bindings added)"
echo

if [ -f "$MPV_SCRIPTS_DIR/archive_movie_enhanced.lua" ]; then
    echo "  📄 $MPV_SCRIPTS_DIR/archive_movie_enhanced.lua"
    echo "  📄 $MPV_SCRIPTS_DIR/delete_movie_enhanced.lua"
fi

if [ -f "$MPV_SCRIPTS_DIR/archive_movie_native.lua" ]; then
    echo "  📄 $MPV_SCRIPTS_DIR/archive_movie_native.lua"
    echo "  📄 $MPV_SCRIPTS_DIR/delete_movie_native.lua"
fi

echo
echo -e "${BLUE}Usage:${NC}"
echo "  🎬 Open videos in MPV player"
echo "  📦 Press 'a' to archive current movie"
echo "  🗑️  Press 'd' to mark current movie for deletion"
echo "  ⏭️  Scripts automatically skip to next video on success"
echo
echo -e "${YELLOW}Note: Make sure Movie Thumbnailer server is running at $HOST:$PORT${NC}"
echo

# Test connection
echo -e "${YELLOW}Testing connection to Movie Thumbnailer API...${NC}"
if command -v curl >/dev/null 2>&1; then
    if curl -s --connect-timeout 5 "http://$HOST:$PORT/api/stats" >/dev/null 2>&1; then
        echo -e "${GREEN}✅ Connection successful!${NC}"
    else
        echo -e "${RED}❌ Could not connect to Movie Thumbnailer API${NC}"
        echo "Make sure the server is running and accessible at http://$HOST:$PORT"
    fi
else
    echo -e "${YELLOW}⚠️  curl not found. Please install curl for API functionality.${NC}"
fi

# Check for Lua HTTP libraries
echo
echo -e "${YELLOW}Checking for native Lua HTTP libraries...${NC}"
if command -v luarocks >/dev/null 2>&1; then
    echo -e "${BLUE}LuaRocks found. You can install HTTP libraries for better performance:${NC}"
    echo "  luarocks install luasocket    # Recommended for most users"
    echo "  luarocks install http         # Modern alternative with HTTP/2 support"
else
    echo -e "${YELLOW}LuaRocks not found. Native HTTP scripts will fall back to curl.${NC}"
    echo "Consider installing LuaRocks for better performance: https://luarocks.org/"
fi

echo
echo -e "${GREEN}Setup complete! Enjoy using Movie Thumbnailer with MPV! 🎉${NC}"
