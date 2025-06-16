#!/bin/bash

# Detailed test for delete undo functionality
set -e

BASE_URL="http://localhost:8080"
COOKIE_JAR="/tmp/delete_debug.txt"

echo "=== Detailed Delete Undo Test ==="

# Clean start
rm -f "$COOKIE_JAR"
curl -s -X POST "$BASE_URL/reset-views" > /dev/null

echo "1. Load first slide"
slide_html=$(curl -s -c "$COOKIE_JAR" "$BASE_URL/slideshow")
slide_id=$(echo "$slide_html" | grep -oP '/slideshow/next\?current=\K\d+' | head -1)
movie_path=$(echo "$slide_html" | grep -oP 'name="path" value="\K[^"]*')
echo "   Slide ID: $slide_id"
echo "   Movie Path: $movie_path"

# Check initial undo button state
if echo "$slide_html" | grep -q "nav-button undo disabled"; then
    echo "   ✓ Initial undo button: disabled"
elif echo "$slide_html" | grep -q "nav-button undo navigation-undo"; then
    echo "   ? Initial undo button: navigation-undo (unexpected)"
else
    echo "   ? Initial undo button: unknown state"
fi

echo ""
echo "2. Delete the slide"
delete_response=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST -d "path=$movie_path" "$BASE_URL/slideshow/delete")

# The delete should redirect back to the same slide with pending deletion state
after_delete_html=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$BASE_URL/slideshow?id=$slide_id")

echo "   After delete, checking undo button state:"
if echo "$after_delete_html" | grep -q "nav-button undo delete-undo"; then
    echo "   ✓ Undo button: delete-undo (RED - correct!)"
elif echo "$after_delete_html" | grep -q "nav-button undo navigation-undo"; then
    echo "   ✗ Undo button: navigation-undo (ORANGE - wrong!)"
elif echo "$after_delete_html" | grep -q "nav-button undo disabled"; then
    echo "   ✗ Undo button: disabled (GRAY - wrong!)"
else
    echo "   ? Undo button: unknown state"
fi

# Check if pending deletion message is shown
if echo "$after_delete_html" | grep -q "Pending Deletion"; then
    echo "   ✓ Pending deletion message shown"
else
    echo "   ✗ Pending deletion message NOT shown"
fi

# Check button text
if echo "$after_delete_html" | grep -q "🗑️ Undo Delete"; then
    echo "   ✓ Undo button text: '🗑️ Undo Delete'"
else
    echo "   ✗ Undo button text: NOT '🗑️ Undo Delete'"
    echo "   Actual button text: $(echo "$after_delete_html" | grep -oP '>.*Undo.*<' | head -1)"
fi

echo ""
echo "3. Test undo delete"
undo_response=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" -L "$BASE_URL/slideshow/previous?current=$slide_id")

# After undo, should be back to normal state
if echo "$undo_response" | grep -q "nav-button undo disabled"; then
    echo "   ✓ After undo: button disabled (correct for single slide)"
elif echo "$undo_response" | grep -q "nav-button undo navigation-undo"; then
    echo "   ✓ After undo: navigation-undo (correct if there's history)"
else
    echo "   ? After undo: unknown button state"
fi

if echo "$undo_response" | grep -q "Pending Deletion"; then
    echo "   ✗ After undo: still shows pending deletion"
else
    echo "   ✓ After undo: pending deletion cleared"
fi
