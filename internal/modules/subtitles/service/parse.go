package service

import (
	"path"
	"regexp"
	"strings"
)

// Subtitle formats by media type, as set by the storage. Files of any
// other media type are not subtitles for the module.
const (
	mimeSRT   = "application/x-subrip"
	formatSRT = "srt"
)

// subtitleFormat returns the format of a subtitle file of the media type,
// or false if the media type is not one of a supported subtitle.
func subtitleFormat(mimeType string) (string, bool) {
	if mimeType == mimeSRT {
		return formatSRT, true
	}
	return "", false
}

// stemOf returns the name of the file of p without its extension.
func stemOf(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, path.Ext(base))
}

// tagsAfterStem returns what the name of a subtitle adds to the name of a
// video, both without extension: "" for "Movie" and "Movie", "pt-BR" for
// "Movie" and "Movie.pt-BR". The subtitle matches the video when its name
// is the name of the video, or starts with it followed by a dot (names are
// compared ignoring case).
func tagsAfterStem(videoStem, subtitleStem string) (string, bool) {
	n := len(videoStem)
	if n == 0 || len(subtitleStem) < n || !strings.EqualFold(subtitleStem[:n], videoStem) {
		return "", false
	}
	rest := subtitleStem[n:]
	if rest == "" {
		return "", true
	}
	if rest[0] != '.' {
		return "", false
	}
	return rest[1:], true
}

// tags are the attributes a subtitle name gives to the subtitle.
type tags struct {
	// Language is a BCP 47 tag (e.g. "pt-BR"), empty when unknown.
	Language        string
	Title           string
	Forced          bool
	HearingImpaired bool
}

// languageTag is what a language tag of a file name looks like: a language
// of two or three letters, optionally followed by subtags (script, region).
var languageTag = regexp.MustCompile(`^[a-zA-Z]{2,3}([-_][a-zA-Z0-9]{2,8})*$`)

// parseTags reads the attributes of a subtitle from what follows the name
// of its video, split by dots (e.g. "pt-BR.forced"): "forced" marks a forced
// subtitle, "sdh", "hi" and "cc" a subtitle for the hearing impaired, the
// first token that looks like a language tag is the language, and the
// remaining tokens are the title.
func parseTags(rest string) tags {
	var t tags
	var title []string
	for tok := range strings.SplitSeq(rest, ".") {
		switch lower := strings.ToLower(tok); {
		case tok == "":
		case lower == "forced":
			t.Forced = true
		case lower == "sdh" || lower == "hi" || lower == "cc":
			t.HearingImpaired = true
		case t.Language == "" && languageTag.MatchString(tok):
			t.Language = normalizeLanguage(tok)
		default:
			title = append(title, tok)
		}
	}
	t.Title = strings.Join(title, " ")
	return t
}

// normalizeLanguage writes a language tag in the BCP 47 way: lowercase
// language, title case script, uppercase region, separated by hyphens.
func normalizeLanguage(tag string) string {
	parts := strings.Split(strings.ReplaceAll(tag, "_", "-"), "-")
	for i, p := range parts {
		switch {
		case i == 0:
			parts[i] = strings.ToLower(p)
		case len(p) == 2:
			parts[i] = strings.ToUpper(p)
		case len(p) == 4:
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		default:
			parts[i] = strings.ToLower(p)
		}
	}
	return strings.Join(parts, "-")
}
