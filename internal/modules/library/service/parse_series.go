package service

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// parsedEpisode is what is known about an episode from its path.
type parsedEpisode struct {
	SeriesTitle string
	// SeriesYear is zero when unknown.
	SeriesYear int
	// Season is 0 for the specials.
	Season int
	// Number is nil when the file names no episode number. EndNumber is
	// the last episode of a file holding several (e.g. "S01E01E02"), or nil.
	Number, EndNumber *int
	Title             string
}

var (
	// seasonEpisodeRe matches "S01E02" (the season and the episode).
	seasonEpisodeRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,4})[\s._-]*e(\d{1,4})`)
	// crossRe matches "1x02" (the season and the episode). The season has
	// at most 2 digits, so that resolutions ("1920x1080") are not matched.
	crossRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(\d{1,2})x(\d{2,3})`)
	// moreEpisodesRe matches each further episode of a multi-episode file,
	// right after the first one: "E02", "-E02", "-S01E02", "-1x02", "-02".
	moreEpisodesRe = regexp.MustCompile(`(?i)^(?:-?(?:s\d{1,4})?e|-?\d{1,2}x|-)(\d{1,4})`)
	// episodeOnlyRe matches an episode without season ("E07", "Ep 7",
	// "Episode 7").
	episodeOnlyRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:episode|ep|e)[\s._-]?(\d{1,4})`)
	// absoluteRe matches the absolute number of an episode, common in
	// anime: "Series - 105" (once bracketed tags are removed).
	absoluteRe = regexp.MustCompile(`(?i)(?:^|\s)-\s+(\d{1,4})(?:v\d)?(?:\s|$)`)
	// leadingNumberRe matches a file named by its number ("105",
	// "05 - Title").
	leadingNumberRe = regexp.MustCompile(`(?i)^(\d{1,4})(?:v\d)?(?:[\s._]*-[\s._]*|[\s._]+|$)`)
	// seasonDirRe and shortSeasonDirRe match season folders: "Season 01",
	// "Season 1", "Temporada 2", "S01".
	seasonDirRe      = regexp.MustCompile(`(?i)^(?:season|series|saison|temporada|staffel|stagione|seizoen)[\s._-]*(\d{1,4})(?:[^0-9].*)?$`)
	shortSeasonDirRe = regexp.MustCompile(`(?i)^s(\d{1,4})(?:[\s._-].*)?$`)
	// specialsDirRe matches the folders of the specials (season 0).
	specialsDirRe = regexp.MustCompile(`(?i)^specials?$`)
	// bracketsRe matches bracketed tags ("[1080p]", "{tmdb-1}", "(2020)").
	bracketsRe = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}|\([^)]*\)`)
	// tagsRe matches the tags of a folder name ("[tvdbid-78804]").
	tagsRe = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
)

// parseEpisode identifies an episode from the path of a file relative to
// its location, e.g. "Breaking Bad (2008)/Season 01/Breaking Bad -
// S01E02.mkv". The series is named by the first folder of the path (or, for
// files at the root, by the start of the file name). The season comes from
// the file name ("S02E05"), then from a season folder ("Season 02"), and
// is 1 otherwise. It returns false for files that are not episodes (not
// videos, extras, or files at the root that name no episode). Years after
// maxYear are not years.
func parseEpisode(p string, maxYear int) (parsedEpisode, bool) {
	name, ok := mainVideo(p)
	if !ok {
		return parsedEpisode{}, false
	}
	var dirs []string
	if dir := path.Dir(p); dir != "." {
		dirs = strings.Split(dir, "/")
	}

	var e parsedEpisode
	season, rest, prefix, found := parseEpisodeNumbers(name, &e)
	switch {
	case len(dirs) > 0:
		e.SeriesTitle, e.SeriesYear = parseSeriesName(dirs[0], maxYear)
	case found && prefix != "":
		e.SeriesTitle, e.SeriesYear = parseSeriesName(prefix, maxYear)
	}
	if e.SeriesTitle == "" {
		return parsedEpisode{}, false
	}

	e.Season = 1
	if season >= 0 {
		e.Season = season
	} else if n, ok := seasonOfDirs(dirs); ok {
		e.Season = n
	}

	if found {
		e.Title = episodeTitle(rest)
	}
	if e.Title == "" {
		e.Title = name
	}
	return e, true
}

// parseEpisodeNumbers finds the episode numbers in a file name, setting
// e.Number and e.EndNumber. It returns the season the name gives (-1 if
// none), the text after the numbers, the text before them, and whether a
// number was found.
func parseEpisodeNumbers(name string, e *parsedEpisode) (season int, rest, prefix string, found bool) {
	for _, re := range []*regexp.Regexp{seasonEpisodeRe, crossRe} {
		if m := re.FindStringSubmatchIndex(name); m != nil {
			season = atoi(name[m[2]:m[3]])
			n := atoi(name[m[4]:m[5]])
			e.Number = &n
			end, after := moreEpisodes(name[m[5]:], n)
			if end > n {
				e.EndNumber = &end
			}
			return season, after, name[:m[0]], true
		}
	}
	if m := episodeOnlyRe.FindStringSubmatchIndex(name); m != nil && !isDigitAt(name, m[3]) {
		n := atoi(name[m[2]:m[3]])
		e.Number = &n
		end, after := moreEpisodes(name[m[3]:], n)
		if end > n {
			e.EndNumber = &end
		}
		return -1, after, name[:m[0]], true
	}

	// Absolute numbers are searched without the bracketed tags, which
	// often hold numbers (resolutions, checksums, years).
	clean := strings.TrimSpace(spacesRe.ReplaceAllString(bracketsRe.ReplaceAllString(name, " "), " "))
	if m := leadingNumberRe.FindStringSubmatchIndex(clean); m != nil && !isYear(clean[m[2]:m[3]]) {
		n := atoi(clean[m[2]:m[3]])
		e.Number = &n
		return -1, clean[m[1]:], "", true
	}
	if m := absoluteRe.FindStringSubmatchIndex(clean); m != nil {
		n := atoi(clean[m[2]:m[3]])
		e.Number = &n
		return -1, clean[m[1]:], clean[:m[0]], true
	}
	return -1, "", "", false
}

// moreEpisodes reads the further episodes of a multi-episode file in s,
// which follows the first episode (first). It returns the last episode
// and the text after it.
func moreEpisodes(s string, first int) (int, string) {
	last := first
	for {
		m := moreEpisodesRe.FindStringSubmatchIndex(s)
		if m == nil || isAlnumAt(s, m[1]) {
			return last, s
		}
		n := atoi(s[m[2]:m[3]])
		if n <= last {
			return last, s
		}
		last, s = n, s[m[1]:]
	}
}

// seasonOfDirs returns the season of the deepest season folder below the
// series folder (dirs[0]).
func seasonOfDirs(dirs []string) (int, bool) {
	for i := len(dirs) - 1; i >= 1; i-- {
		d := strings.TrimSpace(dirs[i])
		if specialsDirRe.MatchString(d) {
			return 0, true
		}
		for _, re := range []*regexp.Regexp{seasonDirRe, shortSeasonDirRe} {
			if m := re.FindStringSubmatch(d); m != nil {
				return atoi(m[1]), true
			}
		}
	}
	return 0, false
}

// parseSeriesName extracts the title and the year of a series from the
// name of its folder (e.g. "Doctor Who (2005) [tvdbid-78804]").
func parseSeriesName(name string, maxYear int) (string, int) {
	// Provider ids and other tags are not part of the name; the year is
	// kept, as it tells apart series with the same title.
	m := parseName(tagsRe.ReplaceAllString(name, " "), maxYear)
	if m.Title == "" {
		return cleanTitle(strings.TrimSpace(name)), 0
	}
	return m.Title, m.Year
}

// episodeTitle extracts the title of an episode from the text after its
// numbers (e.g. " - Pilot [1080p]").
func episodeTitle(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if !strings.Contains(strings.TrimSpace(s), " ") {
		s = strings.ReplaceAll(s, ".", " ")
	}
	if i := strings.IndexAny(s, "[{"); i >= 0 {
		s = s[:i]
	}
	s = cleanTitle(s)
	if loc := releaseTagRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	return cleanTitle(s)
}

// seriesKey is the key series are matched by: the lower-case title with
// only letters and digits, so that "Avatar: The Last Airbender" and
// "Avatar The Last Airbender" are the same series.
func seriesKey(title string) string {
	fields := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(fields, " ")
}

// naturalKey is a key that sorts names naturally: "ep2" before "ep10".
// Numbers are padded with zeros to 10 digits.
func naturalKey(name string) string {
	var b strings.Builder
	s := strings.ToLower(name)
	for i := 0; i < len(s); {
		if !isDigit(s[i]) {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		digits := strings.TrimLeft(s[i:j], "0")
		if len(digits) < 10 {
			b.WriteString(strings.Repeat("0", 10-len(digits)))
		}
		b.WriteString(digits)
		i = j
	}
	return b.String()
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func isDigitAt(s string, i int) bool { return i < len(s) && isDigit(s[i]) }

// isYear reports whether a number of a file name is more likely a year
// than an episode.
func isYear(s string) bool {
	n, _ := strconv.Atoi(s)
	return len(s) == 4 && n >= 1900 && n < 2100
}
