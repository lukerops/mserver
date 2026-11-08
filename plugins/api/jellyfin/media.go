package jellyfin

import (
	"path"
	"strconv"
	"strings"

	libraryrouter "github.com/lukerops/mserver/internal/modules/library/router"
)

// ticksPerMs converts milliseconds to Jellyfin ticks (100 ns).
const ticksPerMs = 10_000

// newItem returns an item with the fields every item has, arrays and maps
// initialized.
func (p *plugin) newItem(id, name string) BaseItemDto {
	return BaseItemDto{
		Name:                     name,
		ServerId:                 serverID,
		Id:                       id,
		SortName:                 strings.ToLower(name),
		ExternalUrls:             []any{},
		EnableMediaSourceDisplay: true,
		Taglines:                 []string{},
		Genres:                   []string{},
		PlayAccess:               "Full",
		RemoteTrailers:           []any{},
		ProviderIds:              map[string]string{},
		People:                   []any{},
		Studios:                  []any{},
		GenreItems:               []any{},
		UserData:                 UserItemDataDto{Key: id, ItemId: id},
		DisplayPreferencesId:     id,
		Tags:                     []string{},
		ImageTags:                map[string]string{},
		BackdropImageTags:        []string{},
		ImageBlurHashes:          map[string]map[string]string{},
		LocationType:             "FileSystem",
		LockedFields:             []string{},
		Chapters:                 []any{},
	}
}

// libraryItem maps a library to a Jellyfin collection folder (a "view").
func (p *plugin) libraryItem(l libraryrouter.LibraryV1) BaseItemDto {
	item := p.newItem(jellyfinID(l.ID.String()), l.Name)
	created := l.CreatedAt
	item.DateCreated = &created
	item.IsFolder = true
	item.Type = "CollectionFolder"
	item.CollectionType = l.MediaType // "movies" in both
	item.MediaType = "Unknown"
	return item
}

// movieItem maps a movie to a Jellyfin movie. The media sources and
// streams are included only when full is set.
func (p *plugin) movieItem(m libraryrouter.MovieV1, full bool) BaseItemDto {
	id := jellyfinID(m.ID.String())
	item := p.newItem(id, m.Title)
	created := m.CreatedAt
	item.DateCreated = &created
	item.SortName = m.SortTitle
	item.ProductionYear = m.Year
	item.ParentId = jellyfinID(m.LibraryID.String())
	item.Type = "Movie"
	item.MediaType = "Video"
	item.VideoType = "VideoFile"
	item.Container = container(m.Path)
	item.Path = m.Path

	source := MediaSourceInfo{
		Protocol:             "File",
		Id:                   id,
		Path:                 m.Path,
		Type:                 "Default",
		Container:            item.Container,
		Name:                 strings.TrimSuffix(path.Base(m.Path), path.Ext(m.Path)),
		SupportsDirectStream: true,
		SupportsDirectPlay:   true,
		SupportsProbing:      true,
		VideoType:            "VideoFile",
		MediaStreams:         []MediaStream{},
		MediaAttachments:     []any{},
		Formats:              []string{},
	}
	if info := m.MediaInfo; info != nil {
		item.RunTimeTicks = info.DurationMs * ticksPerMs
		source.RunTimeTicks = item.RunTimeTicks
		source.Size = info.Size
		source.Bitrate = info.BitRate
		var defaultAudio bool // the chosen audio stream is flagged default
		for _, s := range info.Streams {
			ms := mediaStream(s)
			source.MediaStreams = append(source.MediaStreams, ms)
			switch s.Type {
			case "video":
				if item.Width == 0 {
					item.Width, item.Height = s.Width, s.Height
				}
			case "audio":
				if source.DefaultAudioStreamIndex == nil || (s.Default && !defaultAudio) {
					source.DefaultAudioStreamIndex = &ms.Index
					defaultAudio = s.Default
				}
			case "subtitle":
				item.HasSubtitles = true
			}
		}
		item.IsHD = item.Height >= 720
	}
	if full {
		item.MediaSources = []MediaSourceInfo{source}
		item.MediaStreams = source.MediaStreams
	}
	return item
}

// container is the container of a file the way Jellyfin names it: its
// extension.
func container(p string) string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(p)), ".")
}

func mediaStream(s libraryrouter.StreamV1) MediaStream {
	ms := MediaStream{
		Codec:              s.Codec,
		Language:           s.Language,
		ColorTransfer:      s.ColorTransfer,
		ColorPrimaries:     s.ColorPrimaries,
		ColorSpace:         s.ColorSpace,
		AudioSpatialFormat: "None",
		Title:              s.Title,
		IsInterlaced:       s.Interlaced,
		BitRate:            s.BitRate,
		BitDepth:           s.BitDepth,
		ChannelLayout:      s.ChannelLayout,
		Channels:           s.Channels,
		SampleRate:         s.SampleRate,
		IsDefault:          s.Default,
		IsForced:           s.Forced,
		IsHearingImpaired:  s.HearingImpaired,
		Height:             s.Height,
		Width:              s.Width,
		AverageFrameRate:   s.FrameRate,
		RealFrameRate:      s.FrameRate,
		Profile:            s.Profile,
		AspectRatio:        s.AspectRatio,
		Index:              s.Index,
		PixelFormat:        s.PixelFormat,
		Level:              s.Level,
	}
	switch s.Type {
	case "video":
		ms.Type = "Video"
		ms.VideoRange, ms.VideoRangeType = videoRange(s.ColorTransfer)
	case "audio":
		ms.Type = "Audio"
	case "subtitle":
		ms.Type = "Subtitle"
		ms.IsTextSubtitleStream = textSubtitleCodecs[s.Codec]
		ms.SupportsExternalStream = ms.IsTextSubtitleStream
	default:
		ms.Type = "Data"
	}
	ms.DisplayTitle = displayTitle(s, ms.VideoRange)
	return ms
}

var textSubtitleCodecs = map[string]bool{
	"ass": true, "mov_text": true, "srt": true, "ssa": true, "subrip": true,
	"text": true, "ttml": true, "webvtt": true,
}

// videoRange classifies the dynamic range of a video by its transfer
// characteristics. Dolby Vision is not detected.
func videoRange(transfer string) (videoRange, rangeType string) {
	switch transfer {
	case "smpte2084":
		return "HDR", "HDR10"
	case "arib-std-b67":
		return "HDR", "HLG"
	}
	return "SDR", "SDR"
}

// displayTitle describes a stream the way Jellyfin does, e.g. "1080p H264
// SDR" or "English - AAC - Stereo - Default".
func displayTitle(s libraryrouter.StreamV1, videoRange string) string {
	codec := strings.ToUpper(s.Codec)
	switch s.Type {
	case "video":
		parts := []string{}
		if r := resolution(s.Width, s.Height, s.Interlaced); r != "" {
			parts = append(parts, r)
		}
		return strings.Join(append(parts, codec, videoRange), " ")
	case "audio", "subtitle":
		var parts []string
		if s.Title != "" {
			parts = append(parts, s.Title)
		} else if l := languageName(s.Language); l != "" {
			parts = append(parts, l)
		}
		parts = append(parts, codec)
		if s.Type == "audio" {
			if c := channelsName(s.Channels, s.ChannelLayout); c != "" {
				parts = append(parts, c)
			}
		}
		if s.Default {
			parts = append(parts, "Default")
		}
		if s.Forced {
			parts = append(parts, "Forced")
		}
		return strings.Join(parts, " - ")
	}
	return codec
}

// resolutions are the resolution names of Jellyfin, by maximum width and
// height.
var resolutions = []struct {
	width, height int
	name          string
}{
	{256, 144, "144"}, {426, 240, "240"}, {640, 360, "360"}, {682, 384, "384"},
	{720, 404, "404"}, {854, 480, "480"}, {960, 544, "540"}, {1024, 576, "576"},
	{1280, 962, "720"}, {2560, 1440, "1080"}, {4096, 3072, "4K"}, {8192, 6144, "8K"},
}

func resolution(width, height int, interlaced bool) string {
	if width == 0 && height == 0 {
		return ""
	}
	scan := "p"
	if interlaced {
		scan = "i"
	}
	for _, r := range resolutions {
		if width <= r.width && height <= r.height {
			if strings.HasSuffix(r.name, "K") {
				return r.name
			}
			return r.name + scan
		}
	}
	return ""
}

func channelsName(channels int, layout string) string {
	switch channels {
	case 0:
		return ""
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	}
	if layout != "" {
		return layout
	}
	return strconv.Itoa(channels) + " ch"
}

// languages names the most common ISO 639-2 codes; others are shown in
// upper case.
var languages = map[string]string{
	"ara": "Arabic", "chi": "Chinese", "zho": "Chinese", "cze": "Czech", "ces": "Czech",
	"dan": "Danish", "dut": "Dutch", "nld": "Dutch", "eng": "English", "fin": "Finnish",
	"fre": "French", "fra": "French", "ger": "German", "deu": "German", "gre": "Greek",
	"ell": "Greek", "heb": "Hebrew", "hin": "Hindi", "hun": "Hungarian", "ita": "Italian",
	"jpn": "Japanese", "kor": "Korean", "nor": "Norwegian", "pol": "Polish",
	"por": "Portuguese", "pob": "Portuguese (Brazil)", "rum": "Romanian", "ron": "Romanian",
	"rus": "Russian", "spa": "Spanish", "swe": "Swedish", "tha": "Thai", "tur": "Turkish",
	"ukr": "Ukrainian", "vie": "Vietnamese",
}

func languageName(code string) string {
	if code == "" || code == "und" {
		return ""
	}
	if name, ok := languages[strings.ToLower(code)]; ok {
		return name
	}
	return strings.ToUpper(code)
}
