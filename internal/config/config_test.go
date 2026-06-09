package config

import (
	"os"
	"testing"
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
