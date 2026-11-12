package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The media info is saved as JSON. The languages of its audio and subtitle
// streams are also saved as lowercase tags, sorted and delimited by commas
// (",eng,por,"; empty when there are none), so that the lists filter by
// language with a LIKE that works on SQLite and Postgres.

const (
	streamAudio    = "audio"
	streamSubtitle = "subtitle"
)

// encodeMediaInfo returns the JSON of info and its language columns. The
// JSON is NULL until the file is analyzed.
func encodeMediaInfo(info *MediaInfo) (data sql.NullString, audio, subtitle string, err error) {
	if info == nil {
		return sql.NullString{}, "", "", nil
	}
	b, err := json.Marshal(info)
	if err != nil {
		return sql.NullString{}, "", "", fmt.Errorf("store: encode media info: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true},
		languageList(info, streamAudio), languageList(info, streamSubtitle), nil
}

func decodeMediaInfo(data sql.NullString) (*MediaInfo, error) {
	if !data.Valid {
		return nil, nil
	}
	info := new(MediaInfo)
	if err := json.Unmarshal([]byte(data.String), info); err != nil {
		return nil, fmt.Errorf("store: decode media info: %w", err)
	}
	return info, nil
}

// encodeSubtitles returns the JSON of the external subtitles, NULL when
// there are none.
func encodeSubtitles(subs []ExternalSubtitle) (sql.NullString, error) {
	if len(subs) == 0 {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(subs)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("store: encode external subtitles: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func decodeSubtitles(data sql.NullString) ([]ExternalSubtitle, error) {
	if !data.Valid {
		return nil, nil
	}
	var subs []ExternalSubtitle
	if err := json.Unmarshal([]byte(data.String), &subs); err != nil {
		return nil, fmt.Errorf("store: decode external subtitles: %w", err)
	}
	return subs, nil
}

// languageList returns the languages of the streams of a type, as saved in
// the language columns.
func languageList(info *MediaInfo, typ string) string {
	var langs []string
	for i := range info.Streams {
		s := &info.Streams[i]
		if s.Type != typ || s.Language == "" {
			continue
		}
		langs = append(langs, strings.ToLower(strings.ReplaceAll(s.Language, ",", "")))
	}
	if len(langs) == 0 {
		return ""
	}
	slices.Sort(langs)
	langs = slices.Compact(langs)
	return "," + strings.Join(langs, ",") + ","
}

// likeEscaper escapes the wildcards of LIKE, with the backslash as the
// escape character.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// languageFilter returns the condition that selects the rows whose language
// column has one of the languages (case-insensitive).
func languageFilter(args *[]any, column string, languages []string) string {
	conds := make([]string, len(languages))
	for i, l := range languages {
		pattern := "%," + likeEscaper.Replace(strings.ToLower(l)) + ",%"
		conds[i] = column + " LIKE " + arg(args, pattern) + ` ESCAPE '\'`
	}
	return "(" + strings.Join(conds, " OR ") + ")"
}
