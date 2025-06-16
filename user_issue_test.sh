#!/bin/bash

# Simple test to reproduce the issues mentioned by the user
set -e

BASE_URL="http://localhost:8080"
COOKIE_JAR="/tmp/user_issue_test.txt"

echo "=== Testing User Reported Issues ==="

# Clean start
rm -f "$COOKIE_JAR"
curl -s -X POST "$BASE_URL/reset-views" > /dev/null

echo "1. Fresh slideshow load - check counter"
slide1_html=$(curl -s -c "$COOKIE_JAR" "$BASE_URL/slideshow")
counter1=$(echo "$slide1_html" | grep -o "Thumbnail [0-9]* of [0-9]*" || echo "No counter found")
echo "   Counter: $counter1"

# Extract slide ID
slide1_id=$(echo "$slide1_html" | grep -oP '/slideshow/next\?current=\K\d+' | head -1)
echo "   Slide 1 ID: $slide1_id"

# Check undo button state
if echo "$slide1_html" | grep -q "nav-button undo disabled"; then
    echo "   ✓ Undo button is disabled (correct for fresh start)"
elif echo "$slide1_html" | grep -q "nav-button undo navigation-undo"; then
    echo "   ✗ Undo button shows navigation-undo (should be disabled)"
else
    echo "   ? Undo button state unclear"
fi

echo ""
echo "2. Navigate to next slide - check counter and undo"
slide2_html=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" -L "$BASE_URL/slideshow/next?current=$slide1_id")
counter2=$(echo "$slide2_html" | grep -o "Thumbnail [0-9]* of [0-9]*" || echo "No counter found")
echo "   Counter after next: $counter2"

slide2_id=$(echo "$slide2_html" | grep -oP '/slideshow/next\?current=\K\d+' | head -1)
echo "   Slide 2 ID: $slide2_id"

# Check if we got a different slide
if [ "$slide1_id" != "$slide2_id" ]; then
    echo "   ✓ Got different slide (navigation working)"
else
    echo "   ✗ Same slide ID (navigation issue)"
fi

# Check undo button state after navigation
if echo "$slide2_html" | grep -q "nav-button undo disabled"; then
    echo "   ✗ Undo button still disabled (should be enabled after navigation)"
elif echo "$slide2_html" | grep -q "nav-button undo navigation-undo"; then
    echo "   ✓ Undo button shows navigation-undo (correct after navigation)"
else
    echo "   ? Undo button state unclear after navigation"
fi

echo ""
echo "3. Test delete functionality"
delete_response=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST -d "path=$(echo "$slide2_html" | grep -oP 'name="path" value="\K[^"]*')" "$BASE_URL/slideshow/delete")
slide2_after_delete_html=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$BASE_URL/slideshow?id=$slide2_id")

# Check undo button state after delete
if echo "$slide2_after_delete_html" | grep -q "nav-button undo delete-undo"; then
    echo "   ✓ Undo button shows delete-undo after deletion"
elif echo "$slide2_after_delete_html" | grep -q "nav-button undo disabled"; then
    echo "   ✗ Undo button still disabled after deletion"
else
    echo "   ? Undo button state unclear after deletion"
fi

echo ""
echo "Issues summary:"
echo "- Counter stuck at 1: $(echo "$counter1 -> $counter2" | grep -q "1 of.*1 of" && echo "YES" || echo "NO")"
echo "- Undo not working after navigation: $(echo "$slide2_html" | grep -q "disabled" && echo "YES" || echo "NO")"
echo "- Delete undo not showing: $(echo "$slide2_after_delete_html" | grep -q "delete-undo" && echo "NO" || echo "YES")"
