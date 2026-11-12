package jellyfin

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
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
func (p *plugin) libraryItem(l *libraryv1.Library) BaseItemDto {
	item := p.newItem(jellyfinID(l.GetId()), l.GetName())
	created := l.GetCreatedAt().AsTime()
	item.DateCreated = &created
	item.IsFolder = true
	item.Type = "CollectionFolder"
	item.CollectionType = l.GetMediaType() // "movies" and "tvshows" in both
	item.MediaType = "Unknown"
	return item
}

// movieItem maps a movie to a Jellyfin movie. The media sources and
// streams are included only when full is set.
func (p *plugin) movieItem(m *libraryv1.Movie, full bool) BaseItemDto {
	item := p.videoItem(m.GetId(), m.GetTitle(), m.GetPath(), m.GetCreatedAt().AsTime(), m.GetMediaInfo(), m.GetExternalSubtitles(), full)
	item.SortName = m.GetSortTitle()
	item.ProductionYear = int(m.GetYear())
	item.ParentId = jellyfinID(m.GetLibraryId())
	item.Type = "Movie"
	return item
}

// seriesItem maps a series to a Jellyfin series: a folder of seasons.
func (p *plugin) seriesItem(s *libraryv1.Series) BaseItemDto {
	item := p.newItem(jellyfinID(s.GetId()), s.GetTitle())
	created := s.GetCreatedAt().AsTime()
	item.DateCreated = &created
	item.SortName = s.GetSortTitle()
	item.ProductionYear = int(s.GetYear())
	item.ParentId = jellyfinID(s.GetLibraryId())
	item.IsFolder = true
	item.Type = "Series"
	item.MediaType = "Unknown"
	seasons, episodes := int(s.GetSeasonCount()), int(s.GetEpisodeCount())
	item.ChildCount = &seasons
	item.RecursiveItemCount = &episodes
	item.UserData.UnplayedItemCount = &episodes
	return item
}

// seasonItem maps a season to a Jellyfin season: a folder of episodes.
func (p *plugin) seasonItem(s *libraryv1.Season) BaseItemDto {
	number, episodes := int(s.GetNumber()), int(s.GetEpisodeCount())
	item := p.newItem(jellyfinID(s.GetId()), seasonName(number))
	created := s.GetCreatedAt().AsTime()
	item.DateCreated = &created
	// Sorted by number: "Season 10" after "Season 9".
	item.SortName = fmt.Sprintf("%04d", number)
	item.ProductionYear = int(s.GetSeriesYear())
	item.ParentId = jellyfinID(s.GetSeriesId())
	item.IsFolder = true
	item.Type = "Season"
	item.MediaType = "Unknown"
	item.IndexNumber = &number
	item.SeriesId = jellyfinID(s.GetSeriesId())
	item.SeriesName = s.GetSeriesTitle()
	item.ChildCount = &episodes
	item.RecursiveItemCount = &episodes
	item.UserData.UnplayedItemCount = &episodes
	return item
}

// episodeItem maps an episode to a Jellyfin episode. The media sources
// and streams are included only when full is set.
func (p *plugin) episodeItem(e *libraryv1.Episode, full bool) BaseItemDto {
	item := p.videoItem(e.GetId(), e.GetTitle(), e.GetPath(), e.GetCreatedAt().AsTime(), e.GetMediaInfo(), e.GetExternalSubtitles(), full)
	item.SortName = episodeSortName(e)
	item.ProductionYear = int(e.GetSeriesYear())
	item.ParentId = jellyfinID(e.GetSeasonId())
	item.Type = "Episode"
	if e.Number != nil {
		number := int(e.GetNumber())
		item.IndexNumber = &number
	}
	if e.EndNumber != nil {
		end := int(e.GetEndNumber())
		item.IndexNumberEnd = &end
	}
	season := int(e.GetSeasonNumber())
	item.ParentIndexNumber = &season
	item.SeriesId = jellyfinID(e.GetSeriesId())
	item.SeriesName = e.GetSeriesTitle()
	item.SeasonId = jellyfinID(e.GetSeasonId())
	item.SeasonName = seasonName(season)
	return item
}

// seasonName names a season the way Jellyfin does.
func seasonName(number int) string {
	if number == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(number)
}

// episodeSortName sorts the episodes of a season by number, the ones
// without number last, by name.
func episodeSortName(e *libraryv1.Episode) string {
	if e.Number == nil {
		return "9999 - " + strings.ToLower(e.GetTitle())
	}
	return fmt.Sprintf("%04d - %s", e.GetNumber(), strings.ToLower(e.GetTitle()))
}

// videoItem maps a video file to a Jellyfin video item, without its type.
// The external subtitles are streams after the ones embedded in the file
// (see externalSubtitleBase). The media sources and streams are included
// only when full is set.
func (p *plugin) videoItem(rawID, name, filePath string, createdAt time.Time, info *libraryv1.MediaInfo, subs []*libraryv1.ExternalSubtitle, full bool) BaseItemDto {
	id := jellyfinID(rawID)
	item := p.newItem(id, name)
	item.DateCreated = &createdAt
	item.MediaType = "Video"
	item.VideoType = "VideoFile"
	item.Container = container(filePath)
	item.Path = filePath

	source := MediaSourceInfo{
		Protocol:             "File",
		Id:                   id,
		Path:                 filePath,
		Type:                 "Default",
		Container:            item.Container,
		Name:                 strings.TrimSuffix(path.Base(filePath), path.Ext(filePath)),
		SupportsDirectStream: true,
		SupportsDirectPlay:   true,
		SupportsProbing:      true,
		VideoType:            "VideoFile",
		MediaStreams:         []MediaStream{},
		MediaAttachments:     []any{},
		Formats:              []string{},
	}
	if info != nil {
		item.RunTimeTicks = info.GetDurationMs() * ticksPerMs
		source.RunTimeTicks = item.RunTimeTicks
		source.Size = info.GetSize()
		source.Bitrate = info.GetBitRate()
		var defaultAudio bool // the chosen audio stream is flagged default
		for _, s := range info.GetStreams() {
			ms := mediaStream(s)
			source.MediaStreams = append(source.MediaStreams, ms)
			switch s.GetType() {
			case "video":
				if item.Width == 0 {
					item.Width, item.Height = int(s.GetWidth()), int(s.GetHeight())
				}
			case "audio":
				if source.DefaultAudioStreamIndex == nil || (s.GetDefault() && !defaultAudio) {
					index := ms.Index
					source.DefaultAudioStreamIndex = &index
					defaultAudio = s.GetDefault()
				}
			case "subtitle":
				item.HasSubtitles = true
			}
		}
		item.IsHD = item.Height >= 720
	}
	base := externalSubtitleBase(info)
	for i, s := range subs {
		source.MediaStreams = append(source.MediaStreams, externalSubtitleStream(id, base+i, s))
		item.HasSubtitles = true
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

func mediaStream(s *libraryv1.Stream) MediaStream {
	ms := MediaStream{
		Codec:              s.GetCodec(),
		Language:           s.GetLanguage(),
		ColorTransfer:      s.GetColorTransfer(),
		ColorPrimaries:     s.GetColorPrimaries(),
		ColorSpace:         s.GetColorSpace(),
		AudioSpatialFormat: "None",
		Title:              s.GetTitle(),
		IsInterlaced:       s.GetInterlaced(),
		BitRate:            s.GetBitRate(),
		BitDepth:           int(s.GetBitDepth()),
		ChannelLayout:      s.GetChannelLayout(),
		Channels:           int(s.GetChannels()),
		SampleRate:         int(s.GetSampleRate()),
		IsDefault:          s.GetDefault(),
		IsForced:           s.GetForced(),
		IsHearingImpaired:  s.GetHearingImpaired(),
		Height:             int(s.GetHeight()),
		Width:              int(s.GetWidth()),
		AverageFrameRate:   s.GetFrameRate(),
		RealFrameRate:      s.GetFrameRate(),
		Profile:            s.GetProfile(),
		AspectRatio:        s.GetAspectRatio(),
		Index:              int(s.GetIndex()),
		PixelFormat:        s.GetPixelFormat(),
		Level:              int(s.GetLevel()),
	}
	switch s.GetType() {
	case "video":
		ms.Type = "Video"
		ms.VideoRange, ms.VideoRangeType = videoRange(s.GetColorTransfer())
	case "audio":
		ms.Type = "Audio"
	case "subtitle":
		ms.Type = "Subtitle"
		ms.IsTextSubtitleStream = textSubtitleCodecs[s.GetCodec()]
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
func displayTitle(s *libraryv1.Stream, videoRange string) string {
	codec := strings.ToUpper(s.GetCodec())
	switch s.GetType() {
	case "video":
		parts := []string{}
		if r := resolution(int(s.GetWidth()), int(s.GetHeight()), s.GetInterlaced()); r != "" {
			parts = append(parts, r)
		}
		return strings.Join(append(parts, codec, videoRange), " ")
	case "audio", "subtitle":
		var parts []string
		if s.GetTitle() != "" {
			parts = append(parts, s.GetTitle())
		} else if l := languageName(s.GetLanguage()); l != "" {
			parts = append(parts, l)
		}
		parts = append(parts, codec)
		if s.GetType() == "audio" {
			if c := channelsName(int(s.GetChannels()), s.GetChannelLayout()); c != "" {
				parts = append(parts, c)
			}
		}
		if s.GetDefault() {
			parts = append(parts, "Default")
		}
		if s.GetForced() {
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
