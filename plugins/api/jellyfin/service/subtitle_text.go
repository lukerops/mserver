package service

import (
	"bytes"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SubtitleConversion is how a subtitle file of the original format is
// served in the requested format.
type SubtitleConversion int

const (
	// SubtitlePassthrough serves the file as it is.
	SubtitlePassthrough SubtitleConversion = iota
	// SubtitleVTT converts the srt to WebVTT.
	SubtitleVTT
	// SubtitleJSON converts the srt to the JSON of the cues.
	SubtitleJSON
)

// SubtitleConvert tells whether a file of the original format can be served
// in the requested format, and how. Any format is served as it is; only
// srt -> vtt (and the JSON of its cues) is converted.
func SubtitleConvert(original, format string) (SubtitleConversion, bool) {
	original, format = SubtitleFormatName(original), SubtitleFormatName(format)
	switch {
	case format == original:
		return SubtitlePassthrough, true
	case original == "srt" && format == "vtt":
		return SubtitleVTT, true
	case original == "srt" && (format == "js" || format == "json"):
		return SubtitleJSON, true
	}
	return 0, false
}

// SubtitleTrackInfo is the JSON of the cues of a subtitle, as the Jellyfin
// server returns it (MediaBrowser.Model.MediaInfo.SubtitleTrackInfo).
type SubtitleTrackInfo struct {
	TrackEvents []SubtitleTrackEvent `json:"TrackEvents"`
}

// SubtitleTrackEvent is one cue of a subtitle, in ticks (100 ns).
type SubtitleTrackEvent struct {
	ID                 string `json:"Id"`
	Text               string `json:"Text"`
	StartPositionTicks int64  `json:"StartPositionTicks"`
	EndPositionTicks   int64  `json:"EndPositionTicks"`
}

// SRTToTrackInfo parses the text of an SRT file into its cues.
func SRTToTrackInfo(srt []byte) SubtitleTrackInfo {
	info := SubtitleTrackInfo{TrackEvents: []SubtitleTrackEvent{}}
	var (
		id         string
		start, end int64
		body       []string
		haveTiming bool
	)
	flush := func() {
		if haveTiming {
			if id == "" {
				id = strconv.Itoa(len(info.TrackEvents) + 1)
			}
			if text := strings.Trim(strings.Join(body, "\n"), "\n"); strings.TrimSpace(text) != "" {
				info.TrackEvents = append(info.TrackEvents, SubtitleTrackEvent{
					ID: id, Text: text, StartPositionTicks: start, EndPositionTicks: end,
				})
			}
		}
		id, start, end, body, haveTiming = "", 0, 0, nil, false
	}
	for _, line := range strings.Split(normalizeSubtitleText(srt), "\n") {
		if s, e, ok := parseSRTTiming(line); ok {
			if haveTiming {
				flush()
			}
			start, end, haveTiming = s, e, true
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !haveTiming {
			// The index of the cue, before its timing.
			if n, err := strconv.Atoi(trimmed); err == nil {
				id = strconv.Itoa(n)
			}
			continue
		}
		if trimmed == "" {
			flush()
			continue
		}
		body = append(body, line)
	}
	flush()
	return info
}

// parseSRTTiming parses a timing line
// (HH:MM:SS,mmm --> HH:MM:SS,mmm, with an optional position after it).
func parseSRTTiming(line string) (start, end int64, ok bool) {
	before, after, found := strings.Cut(line, "-->")
	if !found {
		return 0, 0, false
	}
	after = strings.TrimSpace(after)
	if i := strings.IndexByte(after, ' '); i >= 0 {
		after = after[:i]
	}
	start, okStart := parseSRTTime(before)
	end, okEnd := parseSRTTime(after)
	if !okStart || !okEnd {
		return 0, 0, false
	}
	return start, end, true
}

// parseSRTTime parses an SRT timestamp (HH:MM:SS,mmm) into ticks (100 ns).
func parseSRTTime(v string) (int64, bool) {
	h, rest, ok := strings.Cut(strings.TrimSpace(v), ":")
	if !ok {
		return 0, false
	}
	m, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, false
	}
	s, milli, ok := strings.Cut(rest, ",")
	if !ok {
		s, milli, ok = strings.Cut(rest, ".")
		if !ok {
			return 0, false
		}
	}
	hh, errH := strconv.Atoi(strings.TrimSpace(h))
	mm, errM := strconv.Atoi(strings.TrimSpace(m))
	ss, errS := strconv.Atoi(strings.TrimSpace(s))
	ms, errMS := strconv.Atoi(strings.TrimSpace(milli))
	if errH != nil || errM != nil || errS != nil || errMS != nil {
		return 0, false
	}
	d := time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute +
		time.Duration(ss)*time.Second + time.Duration(ms)*time.Millisecond
	return d.Nanoseconds() / 100, true
}

// normalizeSubtitleText decodes a subtitle file as UTF-8. Text that is not
// UTF-8 is taken as Latin-1, which is how many SRT files are saved. It also
// drops a leading BOM and normalizes the line endings.
func normalizeSubtitleText(srt []byte) string {
	srt = bytes.TrimPrefix(srt, []byte("\xef\xbb\xbf"))
	text := string(srt)
	if !utf8.ValidString(text) {
		runes := make([]rune, len(srt))
		for i, b := range srt {
			runes[i] = rune(b)
		}
		text = string(runes)
	}
	return strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
}

// SRTToVTT converts the text of an SRT file to WebVTT: it adds the header
// and writes the decimal point of the timestamps.
func SRTToVTT(srt []byte) []byte {
	text := normalizeSubtitleText(srt)

	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, " --> ") {
			line = strings.ReplaceAll(line, ",", ".")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(strings.TrimRight(b.String(), "\n") + "\n")
}
