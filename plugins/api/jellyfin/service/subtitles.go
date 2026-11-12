package service

import "strings"

// SubtitleFormatName normalizes the name of a subtitle format: the codecs
// of ffprobe and the file extensions.
func SubtitleFormatName(format string) string {
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
	original = SubtitleFormatName(original)
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
		if SubtitleFormatName(p.Format) == format {
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
// an item to the format the device plays. The streams of the item and of
// its media source share memory, so both change.
func applySubtitleProfile(item *Item, profile *DeviceProfile) {
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
