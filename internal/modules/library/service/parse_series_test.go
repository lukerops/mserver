package service

import (
	"fmt"
	"slices"
	"testing"
)

func TestParseEpisode(t *testing.T) {
	const maxYear = 2028
	n := func(v int) *int { return &v }
	for _, tt := range []struct {
		path        string
		series      string
		year        int
		season      int
		number, end *int
		title       string
	}{
		// Standard naming.
		{"Breaking Bad (2008)/Season 01/Breaking Bad - S01E02 - Cat's in the Bag.mkv", "Breaking Bad", 2008, 1, n(2), nil, "Cat's in the Bag"},
		{"Breaking Bad/Season 2/Breaking.Bad.S02E05.720p.HDTV.x264.mkv", "Breaking Bad", 0, 2, n(5), nil, "Breaking.Bad.S02E05.720p.HDTV.x264"},
		{"Breaking Bad/Breaking.Bad.S03E01.No.Mas.1080p.mkv", "Breaking Bad", 0, 3, n(1), nil, "No Mas"},
		{"Avatar Seven Havens (2026) [tvdbid-460458][tmdbid-284833]/Season 01/Avatar Seven Havens - S01E01 - On the Horizon [WEBDL-1080p][EAC3 5.1][h264]-playWEB.mkv",
			"Avatar Seven Havens", 2026, 1, n(1), nil, "On the Horizon"},
		{"Doctor Who (2005)/Season 1/Doctor Who 1x02 The End of the World.mp4", "Doctor Who", 2005, 1, n(2), nil, "The End of the World"},
		{"The Office (US)/Season 1/The Office (US) - s01e01.mkv", "The Office (US)", 0, 1, n(1), nil, "The Office (US) - s01e01"},
		{"Show/Season 01/Show S01 E03.mkv", "Show", 0, 1, n(3), nil, "Show S01 E03"},
		// The season of the file name wins over the folder.
		{"Show/Season 01/Show - S02E05.mkv", "Show", 0, 2, n(5), nil, "Show - S02E05"},
		// The season of the folder, when the file names only the episode.
		{"Show/Season 03/E07 - Title.mkv", "Show", 0, 3, n(7), nil, "Title"},
		{"Show/Temporada 4/Episode 8.mkv", "Show", 0, 4, n(8), nil, "Episode 8"},
		{"Show/S05/Ep09.mkv", "Show", 0, 5, n(9), nil, "Ep09"},
		{"Show/Disc 1/Season 06/Show - E10.mkv", "Show", 0, 6, n(10), nil, "Show - E10"},
		// A single season, without season folder.
		{"Show/Show - E02.mkv", "Show", 0, 1, n(2), nil, "Show - E02"},
		// Specials.
		{"Show/Season 00/Show S00E01.mkv", "Show", 0, 0, n(1), nil, "Show S00E01"},
		{"Show/Specials/Making Of.mkv", "Show", 0, 0, nil, nil, "Making Of"},
		{"Show/Show S00E03 - Special.mkv", "Show", 0, 0, n(3), nil, "Special"},
		// Multiple episodes.
		{"Show/Season 01/Show S01E01E02.mkv", "Show", 0, 1, n(1), n(2), "Show S01E01E02"},
		{"Show/Season 01/Show S01E01-E02 - Pilot.mkv", "Show", 0, 1, n(1), n(2), "Pilot"},
		{"Show/Season 01/Show S01E03-04 - Two Parts.mkv", "Show", 0, 1, n(3), n(4), "Two Parts"},
		{"Show/Season 01/Show S01E05-S01E07.mkv", "Show", 0, 1, n(5), n(7), "Show S01E05-S01E07"},
		{"Show/Show 2x05-2x06.mkv", "Show", 0, 2, n(5), n(6), "Show 2x05-2x06"},
		{"Show/Season 01/Show S01E08-720p.mkv", "Show", 0, 1, n(8), nil, "Show S01E08-720p"},
		// Absolute numbers (anime).
		{"Naruto/Naruto - 105.mkv", "Naruto", 0, 1, n(105), nil, "Naruto - 105"},
		{"Naruto/Naruto - 106 - The Title [720p].mkv", "Naruto", 0, 1, n(106), nil, "The Title"},
		{"One Piece (1999)/[SubsPlease] One Piece - 1071 (1080p) [ABCD1234].mkv", "One Piece", 1999, 1, n(1071), nil, "[SubsPlease] One Piece - 1071 (1080p) [ABCD1234]"},
		{"Naruto/107.mkv", "Naruto", 0, 1, n(107), nil, "107"},
		{"Naruto/Season 02/05 - Title.mkv", "Naruto", 0, 2, n(5), nil, "Title"},
		// No number.
		{"Show/Season 01/Pilot.mkv", "Show", 0, 1, nil, nil, "Pilot"},
		{"Show/Bonus 10.mkv", "Show", 0, 1, nil, nil, "Bonus 10"},
		{"Show (2020)/Show (2020) 1080p.mkv", "Show", 2020, 1, nil, nil, "Show (2020) 1080p"},
		// Files at the root are named by their series.
		{"Show.Name.S01E02.Title.mkv", "Show Name", 0, 1, n(2), nil, "Title"},
		{"Show Name (2010) - 1x03.mkv", "Show Name", 2010, 1, n(3), nil, "Show Name (2010) - 1x03"},
	} {
		got, ok := parseEpisode(tt.path, maxYear)
		want := parsedEpisode{SeriesTitle: tt.series, SeriesYear: tt.year, Season: tt.season, Number: tt.number, EndNumber: tt.end, Title: tt.title}
		if !ok || fmtEpisode(got) != fmtEpisode(want) {
			t.Errorf("parseEpisode(%q)\n got %s, %v\nwant %s", tt.path, fmtEpisode(got), ok, fmtEpisode(want))
		}
	}
}

func fmtEpisode(e parsedEpisode) string {
	num := func(p *int) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprint(*p)
	}
	return fmt.Sprintf("%q (%d) S%d E%s-%s %q", e.SeriesTitle, e.SeriesYear, e.Season, num(e.Number), num(e.EndNumber), e.Title)
}

func TestParseEpisodeIgnoresNonEpisodes(t *testing.T) {
	for _, p := range []string{
		"Show/Extras/Behind the Scenes.mkv",
		"Show/Season 01/sample.mkv",
		"Show/Season 01/Show S01E01-trailer.mkv",
		// At the root, only files that name an episode are episodes.
		"Pilot.mkv",
		"S01E01.mkv",
	} {
		if got, ok := parseEpisode(p, 2028); ok {
			t.Errorf("parseEpisode(%q) = %s, want not an episode", p, fmtEpisode(got))
		}
	}
}

func TestSeriesKey(t *testing.T) {
	for title, want := range map[string]string{
		"Avatar: The Last Airbender": "avatar the last airbender",
		"Avatar The  Last Airbender": "avatar the last airbender",
		"Grey's Anatomy":             "grey s anatomy",
		"Shōgun":                     "shōgun",
	} {
		if got := seriesKey(title); got != want {
			t.Errorf("seriesKey(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestNaturalKey(t *testing.T) {
	names := []string{"ep10.mkv", "Ep2.mkv", "ep1b.mkv", "ep01.mkv", "bonus.mkv"}
	slices.SortFunc(names, func(a, b string) int {
		ka, kb := naturalKey(a), naturalKey(b)
		if ka != kb {
			if ka < kb {
				return -1
			}
			return 1
		}
		return 0
	})
	if fmt.Sprint(names) != "[bonus.mkv ep01.mkv ep1b.mkv Ep2.mkv ep10.mkv]" {
		t.Errorf("natural order = %v", names)
	}
}
