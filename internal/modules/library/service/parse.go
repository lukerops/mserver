package service

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

// extraDirs hold extras of a movie (trailers, samples...), not movies.
var extraDirs = map[string]bool{
	"behind the scenes": true, "deleted scenes": true, "extras": true,
	"featurettes": true, "interviews": true, "sample": true, "samples": true,
	"scenes": true, "shorts": true, "trailers": true,
}

// extraSuffixes mark extras in file names (e.g. "Movie-trailer.mkv").
var extraSuffixes = []string{
	"-behindthescenes", "-deleted", "-featurette", "-interview", "-sample",
	"-scene", "-short", "-trailer",
}

var (
	yearRe = regexp.MustCompile(`(?:19|20)\d{2}`)
	// releaseTagRe matches the first tag of a release name; everything
	// after it is not part of the title.
	releaseTagRe = regexp.MustCompile(`(?i)(?:^|[\s\[(])(?:2160p|1080p|1080i|720p|576p|480p|4k|uhd|bluray|blu-ray|bdrip|brrip|bdremux|remux|web-?dl|webrip|hdtv|hdrip|dvdrip|dvdscr|dvd|x264|x265|h[\s.]?26[45]|hevc|avc|xvid|divx|hdr|hdr10|10bit|8bit|aac|ac3|dts|truehd|atmos|proper|repack|extended|unrated|remastered|directors\.? cut|imax)(?:$|[\s\])])`)
	spacesRe     = regexp.MustCompile(`\s+`)
)

// parsedMovie is what is known about a movie from its path.
type parsedMovie struct {
	Title string
	// Year is zero when unknown.
	Year int
}

// mainVideo returns the name of the video at p without its extension, or
// false if it is an extra (trailer, sample...). Whether the file is a video
// is not decided here: the storage tells it by the media type.
func mainVideo(p string) (string, bool) {
	ext := path.Ext(p)
	name := strings.TrimSuffix(path.Base(p), ext)
	lower := strings.ToLower(name)
	if lower == "sample" || lower == "trailer" {
		return "", false
	}
	for _, suffix := range extraSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return "", false
		}
	}
	for d := range strings.SplitSeq(path.Dir(p), "/") {
		if extraDirs[strings.ToLower(d)] {
			return "", false
		}
	}
	return name, true
}

// parseMovie identifies a movie from the path of a file relative to its
// location, e.g. "Movies/Alien (1979)/Alien.mkv" or
// "Alien.1979.1080p.BluRay.x264.mkv". It returns false for extras such as
// trailers. The path must be of a video. Years after maxYear
// are not years (e.g. "Blade Runner 2049").
func parseMovie(p string, maxYear int) (parsedMovie, bool) {
	name, ok := mainVideo(p)
	if !ok {
		return parsedMovie{}, false
	}
	dir := path.Dir(p)

	// A folder named "Title (Year)" names the movie better than the file
	// inside it (e.g. "Alien (1979)/movie.mkv").
	if dir != "." {
		if m := parseName(path.Base(dir), maxYear); m.Year != 0 {
			return m, true
		}
	}
	if m := parseName(name, maxYear); m.Title != "" {
		return m, true
	}
	return parsedMovie{Title: name}, true
}

// parseName extracts the title and the year of a file or folder name.
func parseName(name string, maxYear int) parsedMovie {
	s := strings.ReplaceAll(name, "_", " ")
	if !strings.Contains(s, " ") {
		s = strings.ReplaceAll(s, ".", " ")
	}

	// The year is the last one after the start of the name: titles may
	// start with or contain numbers that look like years ("1917 (2019)",
	// "Blade Runner 2049 (2017)").
	var m parsedMovie
	end := len(s)
	for _, loc := range yearRe.FindAllStringIndex(s, -1) {
		i, j := loc[0], loc[1]
		if i == 0 || isAlnumAt(s, i-1) || isAlnumAt(s, j) {
			continue
		}
		year := atoi(s[i:j])
		if year > maxYear {
			continue
		}
		m.Year, end = year, i
	}
	s = s[:end]
	if m.Year == 0 {
		if loc := releaseTagRe.FindStringIndex(s); loc != nil {
			s = s[:loc[0]]
		}
	}
	m.Title = cleanTitle(s)
	if m.Title == "" {
		m.Year = 0
	}
	return m
}

func cleanTitle(s string) string {
	s = spacesRe.ReplaceAllString(s, " ")
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("-.([{_,", r)
	})
}

func isAlnumAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := rune(s[i])
	return unicode.IsLetter(c) || unicode.IsDigit(c)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// sortTitle is the key movies are sorted by: the lower-case title without
// a leading English article.
func sortTitle(title string) string {
	t := strings.ToLower(title)
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(t, article); ok && rest != "" {
			return rest
		}
	}
	return t
}
