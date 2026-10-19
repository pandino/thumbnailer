package models

import (
	"strings"
	"testing"
)

func TestGetFileSizeFormatted(t *testing.T) {
	testCases := []struct {
		size     int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1099511627776, "1.00 TB"},
		{512, "512 B"},
		{2048, "2.00 KB"},
		{5368709120, "5.00 GB"}, // 5GB
	}

	for _, tc := range testCases {
		thumbnail := &Thumbnail{FileSize: tc.size}
		result := thumbnail.GetFileSizeFormatted()
		if result != tc.expected {
			t.Errorf("GetFileSizeFormatted(%d) = %s; expected %s", tc.size, result, tc.expected)
		}
	}
}

func TestThumbnailMethods(t *testing.T) {
	thumbnail := &Thumbnail{
		Status:   StatusSuccess,
		Viewed:   0,
		Source:   SourceGenerated,
		Duration: 3661, // 1 hour, 1 minute, 1 second
		Width:    1920,
		Height:   1080,
		FileSize: 1073741824, // 1GB
	}

	// Test status methods
	if !thumbnail.IsSuccess() {
		t.Error("Expected thumbnail to be success")
	}
	if thumbnail.IsPending() {
		t.Error("Expected thumbnail not to be pending")
	}
	if thumbnail.IsError() {
		t.Error("Expected thumbnail not to be error")
	}
	if thumbnail.IsDeleted() {
		t.Error("Expected thumbnail not to be deleted")
	}

	// Test viewed methods
	if thumbnail.IsViewed() {
		t.Error("Expected thumbnail not to be viewed initially")
	}
	thumbnail.MarkAsViewed()
	if !thumbnail.IsViewed() {
		t.Error("Expected thumbnail to be viewed after marking")
	}
	thumbnail.ResetViewed()
	if thumbnail.IsViewed() {
		t.Error("Expected thumbnail not to be viewed after reset")
	}

	// Test source methods
	if thumbnail.IsImported() {
		t.Error("Expected thumbnail not to be imported")
	}

	// Test formatting methods
	expectedDuration := "1:01:01"
	if duration := thumbnail.GetDurationFormatted(); duration != expectedDuration {
		t.Errorf("GetDurationFormatted() = %s; expected %s", duration, expectedDuration)
	}

	expectedResolution := "1920x1080"
	if resolution := thumbnail.GetResolution(); resolution != expectedResolution {
		t.Errorf("GetResolution() = %s; expected %s", resolution, expectedResolution)
	}

	expectedFileSize := "1.00 GB"
	if fileSize := thumbnail.GetFileSizeFormatted(); fileSize != expectedFileSize {
		t.Errorf("GetFileSizeFormatted() = %s; expected %s", fileSize, expectedFileSize)
	}
}

func TestValidStatus(t *testing.T) {
	validStatuses := []string{StatusPending, StatusSuccess, StatusError, StatusDeleted}
	for _, status := range validStatuses {
		if !ValidStatus(status) {
			t.Errorf("Expected %s to be a valid status", status)
		}
	}

	invalidStatuses := []string{"invalid", "unknown", ""}
	for _, status := range invalidStatuses {
		if ValidStatus(status) {
			t.Errorf("Expected %s to be an invalid status", status)
		}
	}
}

func TestValidSource(t *testing.T) {
	validSources := []string{SourceGenerated, SourceImported}
	for _, source := range validSources {
		if !ValidSource(source) {
			t.Errorf("Expected %s to be a valid source", source)
		}
	}

	invalidSources := []string{"invalid", "unknown", ""}
	for _, source := range invalidSources {
		if ValidSource(source) {
			t.Errorf("Expected %s to be an invalid source", source)
		}
	}
}

func TestNormalizeTag(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"Foo", "foo", false},
		{"  Big   Bunny \t", "big bunny", false},
		{"", "", true},
		{"   ", "", true},
		{"a,b", "", true},
		{"a/b", "", true},
		{strings.Repeat("x", MaxTagLength), strings.Repeat("x", MaxTagLength), false},
		{strings.Repeat("x", MaxTagLength+1), "", true},
	}

	for _, tc := range testCases {
		got, err := NormalizeTag(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeTag(%q) expected error, got %q", tc.input, got)
			}
			continue
		}
		if err != nil || got != tc.expected {
			t.Errorf("NormalizeTag(%q) = %q, %v; want %q", tc.input, got, err, tc.expected)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{"Foo", "bar", "FOO ", "baz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"foo", "bar", "baz"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("NormalizeTags = %v; want %v", got, want)
	}

	if _, err := NormalizeTags([]string{"ok", ""}); err == nil {
		t.Error("expected error for empty tag")
	}
}
