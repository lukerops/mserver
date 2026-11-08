package service

import "testing"

func TestParseMovie(t *testing.T) {
	const maxYear = 2028
	for _, tt := range []struct {
		path  string
		title string
		year  int
	}{
		{"Alien (1979).mkv", "Alien", 1979},
		{"Movies/Alien (1979)/Alien (1979).mkv", "Alien", 1979},
		{"Alien (1979)/movie.mkv", "Alien", 1979},
		{"Alien.1979.1080p.BluRay.x264-GROUP.mkv", "Alien", 1979},
		{"The_Matrix_1999_720p.mp4", "The Matrix", 1999},
		{"1917 (2019).mkv", "1917", 2019},
		{"1917.mkv", "1917", 0},
		{"2001 A Space Odyssey (1968).mkv", "2001 A Space Odyssey", 1968},
		{"Blade Runner 2049 (2017).mkv", "Blade Runner 2049", 2017},
		{"Blade.Runner.2049.2017.2160p.UHD.BluRay.mkv", "Blade Runner 2049", 2017},
		{"Blade Runner 2049.mkv", "Blade Runner 2049", 0},
		{"Mr. Nobody (2009) [1080p].mkv", "Mr. Nobody", 2009},
		{"Some.Movie.1080p.WEB-DL.H.264.mkv", "Some Movie", 0},
		{"Collection/Some Movie.mkv", "Some Movie", 0},
		{"Amélie (2001).MKV", "Amélie", 2001},
		{"Back to the Future - 1985.avi", "Back to the Future", 1985},
	} {
		got, ok := parseMovie(tt.path, maxYear)
		if !ok || got.Title != tt.title || got.Year != tt.year {
			t.Errorf("parseMovie(%q) = %+v, %v; want %q (%d)", tt.path, got, ok, tt.title, tt.year)
		}
	}
}

func TestParseMovieIgnoresNonMovies(t *testing.T) {
	for _, p := range []string{
		"Alien (1979)/Alien.en.srt",
		"Alien (1979)/poster.jpg",
		"Alien (1979)/Alien-trailer.mkv",
		"Alien (1979)/sample.mkv",
		"Alien (1979)/Extras/Making of.mkv",
		"Alien (1979)/Featurettes/Interview.mp4",
		"Alien (1979)/Sample/alien.sample.mkv",
		"notes",
	} {
		if got, ok := parseMovie(p, 2028); ok {
			t.Errorf("parseMovie(%q) = %+v, want not a movie", p, got)
		}
	}
}

func TestSortTitle(t *testing.T) {
	for title, want := range map[string]string{
		"The Matrix":       "matrix",
		"A Bug's Life":     "bug's life",
		"An American Tail": "american tail",
		"Alien":            "alien",
		"The":              "the",
	} {
		if got := sortTitle(title); got != want {
			t.Errorf("sortTitle(%q) = %q, want %q", title, got, want)
		}
	}
}
