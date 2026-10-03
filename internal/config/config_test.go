package config

import (
	"os"
	"testing"
	"time"
)

func TestGetEnvAsMovieDirs(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		want     []string
	}{
		{
			name:   "default single dir",
			setEnv: false,
			want:   []string{"/movies"},
		},
		{
			name:     "single value",
			envValue: "/data/movies",
			setEnv:   true,
			want:     []string{"/data/movies"},
		},
		{
			name:     "comma separated",
			envValue: "/movies1,/movies2,/movies3",
			setEnv:   true,
			want:     []string{"/movies1", "/movies2", "/movies3"},
		},
		{
			name:     "whitespace trimmed",
			envValue: " /movies1 , /movies2 , /movies3 ",
			setEnv:   true,
			want:     []string{"/movies1", "/movies2", "/movies3"},
		},
		{
			name:     "empty entries dropped",
			envValue: "/movies1,,/movies2",
			setEnv:   true,
			want:     []string{"/movies1", "/movies2"},
		},
		{
			name:     "all empty falls back to default",
			envValue: ",, ,",
			setEnv:   true,
			want:     []string{"/movies"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("MOVIE_INPUT_DIR")
			if tt.setEnv {
				os.Setenv("MOVIE_INPUT_DIR", tt.envValue)
				defer os.Unsetenv("MOVIE_INPUT_DIR")
			}

			got := getEnvAsMovieDirs("MOVIE_INPUT_DIR", "/movies")

			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNewMoviesDirs_DefaultSingleElement(t *testing.T) {
	os.Unsetenv("MOVIE_INPUT_DIR")
	cfg := New()
	if len(cfg.MoviesDirs) != 1 || cfg.MoviesDirs[0] != "/movies" {
		t.Errorf("default MoviesDirs = %v, want [/movies]", cfg.MoviesDirs)
	}
}

func TestInWorkWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, time.UTC) }
	tests := []struct {
		name       string
		start, end int
		t          time.Time
		want       bool
	}{
		{"before window", 11, 20, at(10, 59), false},
		{"window opens", 11, 20, at(11, 0), true},
		{"inside window", 11, 20, at(15, 30), true},
		{"last minute", 11, 20, at(19, 59), true},
		{"window closed", 11, 20, at(20, 0), false},
		{"wrapping late", 22, 6, at(23, 0), true},
		{"wrapping early", 22, 6, at(5, 59), true},
		{"wrapping outside", 22, 6, at(12, 0), false},
		{"end 24", 11, 24, at(23, 59), true},
		{"end 24 outside", 11, 24, at(0, 0), false},
		{"always open", 0, 24, at(3, 0), true},
		{"equal bounds always open", 5, 5, at(3, 0), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{WorkWindowStart: tt.start, WorkWindowEnd: tt.end}
			if got := c.InWorkWindow(tt.t); got != tt.want {
				t.Errorf("InWorkWindow(%v) with %d-%d = %v, want %v", tt.t, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestWorkWindowEndAfter(t *testing.T) {
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	c := &Config{WorkWindowStart: 11, WorkWindowEnd: 20}
	if got, want := c.WorkWindowEndAfter(at(3, 12, 0)), at(3, 20, 0); !got.Equal(want) {
		t.Errorf("same day: got %v, want %v", got, want)
	}
	if got, want := c.WorkWindowEndAfter(at(3, 20, 0)), at(4, 20, 0); !got.Equal(want) {
		t.Errorf("at close: got %v, want %v", got, want)
	}
	always := &Config{WorkWindowStart: 0, WorkWindowEnd: 24}
	if got := always.WorkWindowEndAfter(at(3, 12, 0)); !got.IsZero() {
		t.Errorf("always open: got %v, want zero", got)
	}
}

func TestWorkWindowEnv(t *testing.T) {
	os.Setenv("WORK_WINDOW_START", "8")
	os.Setenv("WORK_WINDOW_END", "99")
	defer os.Unsetenv("WORK_WINDOW_START")
	defer os.Unsetenv("WORK_WINDOW_END")
	c := New()
	if c.WorkWindowStart != 8 || c.WorkWindowEnd != 20 {
		t.Errorf("got %d-%d, want 8-20 (invalid end falls back to default)", c.WorkWindowStart, c.WorkWindowEnd)
	}
}
