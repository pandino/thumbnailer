#!/bin/bash

# Comprehensive test of undo button states
set -e

BASE_URL="http://localhost:8080"
COOKIE_JAR="/tmp/comprehensive_undo_test.txt"

echo "=== Comprehensive Undo Button Test ==="

# Reset and start fresh
curl -s -X POST "$BASE_URL/reset-views" > /dev/null
rm -f "$COOKIE_JAR"

echo "1. Testing disabled undo (no history)"
fresh_response=$(curl -s -c "$COOKIE_JAR" "$BASE_URL/slideshow")
if echo "$fresh_response" | grep -q 'class="nav-button undo disabled"'; then
    echo "   ✓ Disabled undo button found on fresh start"
else
    echo "   ✗ Disabled undo button NOT found on fresh start"
fi

echo "2. Testing navigation undo"
slide1_id=$(echo "$fresh_response" | grep -oP '/slideshow/next\?current=\K\d+' | head -1)
slide2_response=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" -L "$BASE_URL/slideshow/next?current=$slide1_id")

if echo "$slide2_response" | grep -q 'class="nav-button undo navigation-undo"'; then
    echo "   ✓ Navigation undo button styling found"
else
    echo "   ✗ Navigation undo button styling NOT found"
fi

if echo "$slide2_response" | grep -q "← Undo"; then
    echo "   ✓ Navigation undo button text found"
else
    echo "   ✗ Navigation undo button text NOT found"
fi

echo "3. Testing delete undo"
slide2_id=$(echo "$slide2_response" | grep -oP '/slideshow/next\?current=\K\d+' | head -1)
movie_path=$(echo "$slide2_response" | grep -oP 'name="path" value="\K[^"]+'  | head -1)

# Delete the current slide
delete_response=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" -L -X POST -d "path=$movie_path" "$BASE_URL/slideshow/delete")

# Now go back to the deleted slide to see the delete undo button
deleted_slide_response=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$BASE_URL/slideshow?id=$slide2_id")

if echo "$deleted_slide_response" | grep -q 'class="nav-button undo delete-undo"'; then
    echo "   ✓ Delete undo button styling found"
else
    echo "   ✗ Delete undo button styling NOT found"
fi

if echo "$deleted_slide_response" | grep -q "🗑️ Undo Delete"; then
    echo "   ✓ Delete undo button text found"
else
    echo "   ✗ Delete undo button text NOT found"
fi

echo "4. Testing undo delete functionality"
# Click the undo delete button
undo_response=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" -L "$BASE_URL/slideshow/previous?current=$slide2_id")

# After undo delete, should show normal navigation undo again
if echo "$undo_response" | grep -q 'class="nav-button undo navigation-undo"'; then
    echo "   ✓ After undo delete, navigation undo styling restored"
else
    echo "   ✗ After undo delete, navigation undo styling NOT restored"
fi

echo "All tests completed!"
