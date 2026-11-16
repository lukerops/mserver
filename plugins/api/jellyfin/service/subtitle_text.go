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
// in the requested format, and how. Any format is served as it is; srt is
// converted to vtt, and the cues of the text subtitles this plugin parses
// (srt and vtt) are served as JSON.
func SubtitleConvert(original, format string) (SubtitleConversion, bool) {
	original, format = SubtitleFormatName(original), SubtitleFormatName(format)
	switch {
	case format == original:
		return SubtitlePassthrough, true
	case original == "srt" && format == "vtt":
		return SubtitleVTT, true
	case isTextSubtitle(original) && (format == "js" || format == "json"):
		return SubtitleJSON, true
	}
	return 0, false
}

// isTextSubtitle reports whether this plugin can parse the cues of a text
// subtitle format. The client fetches the cues of a text subtitle as JSON
// (replacing the .vtt of the DeliveryUrl with .js).
func isTextSubtitle(format string) bool {
	switch format {
	case "srt", "vtt":
		return true
	}
	return false
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

// SubtitleTrackInfoOf parses the cues of a text subtitle file of the given
// format (srt or vtt).
func SubtitleTrackInfoOf(format string, data []byte) SubtitleTrackInfo {
	if SubtitleFormatName(format) == "vtt" {
		return VTTToTrackInfo(data)
	}
	return SRTToTrackInfo(data)
}

// SRTToTrackInfo parses the text of an SRT file into its cues.
func SRTToTrackInfo(srt []byte) SubtitleTrackInfo {
	return textToTrackInfo(normalizeSubtitleText(srt))
}

// VTTToTrackInfo parses the text of a WebVTT file into its cues.
func VTTToTrackInfo(vtt []byte) SubtitleTrackInfo {
	return textToTrackInfo(normalizeSubtitleText(vtt))
}

// textToTrackInfo parses the cues of a text subtitle (SRT or WebVTT). The
// header blocks of WebVTT (WEBVTT, NOTE, STYLE, REGION) come before the
// first timing and are ignored, as an SRT cue index is.
func textToTrackInfo(text string) SubtitleTrackInfo {
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
	for _, line := range strings.Split(text, "\n") {
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

// parseSRTTime parses a subtitle timestamp into ticks (100 ns). SRT uses
// HH:MM:SS,mmm; WebVTT also allows MM:SS.mmm.
func parseSRTTime(v string) (int64, bool) {
	whole, milli, ok := strings.Cut(strings.TrimSpace(v), ",")
	if !ok {
		whole, milli, ok = strings.Cut(strings.TrimSpace(v), ".")
		if !ok {
			return 0, false
		}
	}
	parts := strings.Split(whole, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, false
	}
	var hh int
	if len(parts) == 3 {
		h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return 0, false
		}
		hh = h
		parts = parts[1:]
	}
	mm, errM := strconv.Atoi(strings.TrimSpace(parts[0]))
	ss, errS := strconv.Atoi(strings.TrimSpace(parts[1]))
	ms, errMS := strconv.Atoi(strings.TrimSpace(milli))
	if errM != nil || errS != nil || errMS != nil {
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
