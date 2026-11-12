package jellyfin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

// maxSubtitleSize is the largest subtitle file converted to another
// format: a text of a few hundred KB at most.
const maxSubtitleSize = 4 << 20

// externalSubtitleBase is the index of the first external subtitle stream
// of a media source: the ones after the streams embedded in the file, so
// that the indices do not repeat.
func externalSubtitleBase(info *libraryv1.MediaInfo) int {
	base := 0
	for _, s := range info.GetStreams() {
		base = max(base, int(s.GetIndex())+1)
	}
	return base
}

// externalSubtitleStream maps a subtitle file beside the video to a
// subtitle stream, delivered by the plugin (see subtitle). itemID is the
// ID of the item, which is also the ID of its only media source.
func externalSubtitleStream(itemID string, index int, s *libraryv1.ExternalSubtitle) MediaStream {
	codec := strings.ToLower(s.GetFormat())
	language, name := subtitleLanguage(s.GetLanguage())

	parts := []string{}
	if name != "" {
		parts = append(parts, name)
	}
	if s.GetTitle() != "" {
		parts = append(parts, s.GetTitle())
	}
	if len(parts) == 0 {
		parts = append(parts, "Unknown")
	}
	parts = append(parts, strings.ToUpper(codec))
	if s.GetForced() {
		parts = append(parts, "Forced")
	}
	if s.GetHearingImpaired() {
		parts = append(parts, "SDH")
	}
	parts = append(parts, "External")

	return MediaStream{
		Codec:                  codec,
		Language:               language,
		AudioSpatialFormat:     "None",
		DisplayTitle:           strings.Join(parts, " - "),
		Title:                  s.GetTitle(),
		IsForced:               s.GetForced(),
		IsHearingImpaired:      s.GetHearingImpaired(),
		Type:                   "Subtitle",
		Index:                  index,
		IsExternal:             true,
		IsTextSubtitleStream:   true,
		SupportsExternalStream: true,
		DeliveryMethod:         "External",
		// The URL carries the format of the file; PlaybackInfo changes it to
		// the format the device plays (see subtitleDeliveryFormat). The
		// segment before Stream is the start position, as in the Jellyfin
		// server.
		DeliveryUrl: fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.%s", itemID, itemID, index, codec),
	}
}

// signSubtitles appends the token of the request to the DeliveryUrl of
// the external subtitle streams, as the Jellyfin server does: clients
// fetch that URL as it is, with no authorization header. It is idempotent,
// because the streams of an item and of its media source share memory.
func signSubtitles(streams []MediaStream, token string) {
	if token == "" {
		return
	}
	for i := range streams {
		if u := streams[i].DeliveryUrl; u != "" && !strings.Contains(u, "?") {
			streams[i].DeliveryUrl = u + "?ApiKey=" + url.QueryEscape(token)
		}
	}
}

// signItem signs the subtitle streams of an item and of its media sources
// (see signSubtitles).
func signItem(item *BaseItemDto, token string) {
	signSubtitles(item.MediaStreams, token)
	for i := range item.MediaSources {
		signSubtitles(item.MediaSources[i].MediaStreams, token)
	}
}

// signItems signs the items of a query result (see signSubtitles).
func signItems(items []any, token string) {
	for _, it := range items {
		if item, ok := it.(BaseItemDto); ok {
			signItem(&item, token) // the streams are shared with the result
		}
	}
}

// iso639_2 maps the ISO 639-1 codes of the most common languages to the
// ISO 639-2 codes Jellyfin uses for the languages of the streams.
var iso639_2 = map[string]string{
	"ar": "ara", "cs": "cze", "da": "dan", "de": "ger", "el": "gre", "en": "eng", "es": "spa",
	"fi": "fin", "fr": "fre", "he": "heb", "hi": "hin", "hu": "hun", "it": "ita", "ja": "jpn",
	"ko": "kor", "nl": "dut", "no": "nor", "pl": "pol", "pt": "por", "ro": "rum", "ru": "rus",
	"sv": "swe", "th": "tha", "tr": "tur", "uk": "ukr", "vi": "vie", "zh": "chi",
}

// subtitleLanguage converts a BCP 47 tag (e.g. "pt-BR") to the ISO 639-2
// code of its language, and to a name for the display. Unknown languages
// keep their code. An empty tag gives empty results.
func subtitleLanguage(tag string) (code, name string) {
	if tag == "" {
		return "", ""
	}
	lang, _, _ := strings.Cut(strings.ToLower(tag), "-")
	code = lang
	if c, ok := iso639_2[lang]; ok {
		code = c
	}
	if strings.EqualFold(tag, "pt-BR") {
		return code, "Portuguese (Brazil)"
	}
	return code, languageName(code)
}

// subtitle serves the subtitle file at
// /Videos/{itemId}/{mediaSourceId}/Subtitles/{index}/Stream.{format} (and
// the same with the start position in ticks before Stream, which is
// ignored: nothing is cut from the start of the video). The index is the
// one of the stream in the media source. The content comes from the
// storage, in the format of the file when the client asks for it (any
// format, see subtitleConversion). Only srt -> vtt is converted, to WebVTT
// or to the JSON of the cues (SubtitleTrackInfo), which jellyfin-web
// fetches from the DeliveryUrl by replacing the extension (.vtt -> .js).
func (p *plugin) subtitle(w http.ResponseWriter, r *http.Request) {
	format, ok := strings.CutPrefix(r.PathValue("file"), "stream.")
	if !ok {
		notFound(w, r, "unsupported subtitle format")
		return
	}
	format = strings.ToLower(format)
	if ticks := r.PathValue("startPositionTicks"); ticks != "" {
		if _, err := strconv.ParseInt(ticks, 10, 64); err != nil {
			notFound(w, r, "invalid startPositionTicks")
			return
		}
	}
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	v, ok := p.lookupVideo(w, r)
	if !ok {
		return
	}
	if source := r.PathValue("mediaSourceId"); !strings.EqualFold(source, v.item.Id) {
		notFound(w, r, "unknown media source")
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < v.subtitleBase || index-v.subtitleBase >= len(v.subtitles) {
		notFound(w, r, "unknown subtitle stream")
		return
	}
	sub := v.subtitles[index-v.subtitleBase]
	conversion, ok := subtitleConvert(sub.GetFormat(), format)
	if !ok {
		notFound(w, r, "unsupported subtitle format")
		return
	}

	// A converted subtitle needs the whole file.
	passthrough := conversion == subtitlePassthrough
	resp := p.fetchFile(w, r, v.item.Id, sub.GetFileId(), passthrough)
	if resp == nil {
		return
	}
	defer resp.Body.Close()
	if passthrough {
		p.copyFile(w, r, resp)
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSubtitleSize+1))
	switch {
	case err != nil:
		p.log.LogAttrs(r.Context(), slog.LevelWarn, "jellyfin: subtitle: read",
			slog.String("file_id", sub.GetFileId()), slog.String("error", err.Error()))
		unavailable(w, r, "storage unavailable")
		return
	case len(data) > maxSubtitleSize:
		notFound(w, r, "subtitle file too large")
		return
	}

	var body []byte
	contentType := "text/vtt; charset=utf-8"
	if conversion == subtitleJSON {
		body, err = json.Marshal(srtToTrackInfo(data))
		if err != nil {
			p.internalError(w, r, "encode subtitle", err)
			return
		}
		contentType = "application/json; charset=utf-8"
	} else {
		body = srtToVTT(data)
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// subtitleConversion is how a subtitle file of the original format is
// served in the requested format.
type subtitleConversion int

const (
	// subtitlePassthrough serves the file as it is.
	subtitlePassthrough subtitleConversion = iota
	// subtitleVTT converts the srt to WebVTT.
	subtitleVTT
	// subtitleJSON converts the srt to the JSON of the cues.
	subtitleJSON
)

// subtitleConvert tells whether a file of the original format can be
// served in the requested format, and how. Any format is served as it is;
// only srt -> vtt (and the JSON of its cues) is converted.
func subtitleConvert(original, format string) (subtitleConversion, bool) {
	original, format = subtitleFormatName(original), subtitleFormatName(format)
	switch {
	case format == original:
		return subtitlePassthrough, true
	case original == "srt" && format == "vtt":
		return subtitleVTT, true
	case original == "srt" && (format == "js" || format == "json"):
		return subtitleJSON, true
	}
	return 0, false
}

// subtitleFormatName normalizes the name of a subtitle format: the codecs
// of ffprobe and the file extensions.
func subtitleFormatName(format string) string {
	switch f := strings.ToLower(format); f {
	case "subrip", "srt":
		return "srt"
	case "webvtt", "vtt":
		return "vtt"
	case "advancedsubstationalpha", "ass":
		return "ass"
	case "substationalpha", "ssa":
		return "ssa"
	default:
		return f
	}
}

// subtitleDeliveryFormat is the format of the DeliveryUrl of a subtitle of
// the original format: the original when the device plays it, vtt when it
// needs the only conversion this plugin does (srt -> vtt), or the original
// when the device sent no subtitle profiles. False means the device plays
// no format this plugin can serve; the caller keeps the original.
func subtitleDeliveryFormat(profiles []SubtitleProfile, original, language string) (string, bool) {
	original = subtitleFormatName(original)
	if len(profiles) == 0 {
		return original, true
	}
	if subtitleProfile(profiles, original, language) {
		return original, true
	}
	if original == "srt" && subtitleProfile(profiles, "vtt", language) {
		return "vtt", true
	}
	return "", false
}

// subtitleProfile reports whether the device plays the format, delivered as
// an external file.
func subtitleProfile(profiles []SubtitleProfile, format, language string) bool {
	for _, p := range profiles {
		if p.Method != "" && !strings.EqualFold(p.Method, "External") {
			continue
		}
		if !subtitleProfileLanguage(p.Language, language) {
			continue
		}
		if subtitleFormatName(p.Format) == format {
			return true
		}
	}
	return false
}

// subtitleProfileLanguage reports whether a subtitle profile accepts the
// language of the stream. An unknown language is "und", as in the Jellyfin
// server.
func subtitleProfileLanguage(profileLanguage, language string) bool {
	if language == "" {
		language = "und"
	}
	return listAccepts(profileLanguage, language)
}

// withSubtitleFormat rewrites the format of a subtitle DeliveryUrl.
func withSubtitleFormat(deliveryURL, format string) string {
	const marker = "/Stream."
	i := strings.LastIndex(deliveryURL, marker)
	if i < 0 {
		return deliveryURL
	}
	return deliveryURL[:i+len(marker)] + format
}

// applySubtitleProfile rewrites the DeliveryUrl of the subtitle streams of
// an item to the format the device plays (see subtitleDeliveryFormat). The
// streams of the item and of its media source share memory, so both change.
func applySubtitleProfile(item *BaseItemDto, profile *DeviceProfile) {
	var profiles []SubtitleProfile
	if profile != nil {
		profiles = profile.SubtitleProfiles
	}
	for i := range item.MediaSources {
		for j := range item.MediaSources[i].MediaStreams {
			s := &item.MediaSources[i].MediaStreams[j]
			if s.Type != "Subtitle" || !s.IsExternal || s.Codec == "" {
				continue
			}
			if format, ok := subtitleDeliveryFormat(profiles, s.Codec, s.Language); ok {
				s.DeliveryUrl = withSubtitleFormat(s.DeliveryUrl, format)
			}
		}
	}
}

// subtitleTrackInfo is the JSON of the cues of a subtitle, as the Jellyfin
// server returns it (MediaBrowser.Model.MediaInfo.SubtitleTrackInfo).
type subtitleTrackInfo struct {
	TrackEvents []subtitleTrackEvent `json:"TrackEvents"`
}

// subtitleTrackEvent is one cue of a subtitle, in ticks (100 ns).
type subtitleTrackEvent struct {
	ID                 string `json:"Id"`
	Text               string `json:"Text"`
	StartPositionTicks int64  `json:"StartPositionTicks"`
	EndPositionTicks   int64  `json:"EndPositionTicks"`
}

// srtToTrackInfo parses the text of an SRT file into its cues.
func srtToTrackInfo(srt []byte) subtitleTrackInfo {
	info := subtitleTrackInfo{TrackEvents: []subtitleTrackEvent{}}
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
				info.TrackEvents = append(info.TrackEvents, subtitleTrackEvent{
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

// srtToVTT converts the text of an SRT file to WebVTT: it adds the header
// and writes the decimal point of the timestamps.
func srtToVTT(srt []byte) []byte {
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
