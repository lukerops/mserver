package service

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

// ExternalSubtitle is a subtitle file beside a video.
type ExternalSubtitle struct {
	ID              string
	FileID          string
	Format          string
	Language        string
	Title           string
	Forced          bool
	HearingImpaired bool
}

// Video is a playable item: a movie or an episode.
type Video struct {
	Item   Item
	FileID string
	// Subtitles are its external subtitle files; the stream of the first
	// has the index SubtitleBase.
	Subtitles    []ExternalSubtitle
	SubtitleBase int
}

// jellyfinID formats a UUID the way Jellyfin does: without hyphens.
func jellyfinID(uuid string) string {
	return strings.ReplaceAll(uuid, "-", "")
}

// libraryItem maps a library to a Jellyfin collection folder (a "view").
func libraryItem(l *libraryv1.Library) Item {
	id := jellyfinID(l.GetId())
	createdAt := l.GetCreatedAt().AsTime()
	return Item{
		Name:                     l.GetName(),
		ServerId:                 ServerID,
		Id:                       id,
		DateCreated:              &createdAt,
		CanDelete:                false,
		CanDownload:              false,
		SortName:                 strings.ToLower(l.GetName()),
		ExternalUrls:             []any{},
		EnableMediaSourceDisplay: true,
		Taglines:                 []string{},
		Genres:                   []string{},
		PlayAccess:               "Full",
		RemoteTrailers:           []any{},
		ProviderIds:              map[string]string{},
		IsFolder:                 true,
		Type:                     "CollectionFolder",
		People:                   []any{},
		Studios:                  []any{},
		GenreItems:               []any{},
		UserData:                 UserItemData{Key: id, ItemId: id},
		DisplayPreferencesId:     id,
		Tags:                     []string{},
		CollectionType:           l.GetMediaType(),
		ImageTags:                map[string]string{},
		BackdropImageTags:        []string{},
		ImageBlurHashes:          map[string]map[string]string{},
		LocationType:             "FileSystem",
		MediaType:                "Unknown",
		LockedFields:             []string{},
		Chapters:                 []any{},
	}
}

// movieItem maps a movie to a Jellyfin movie. The media sources and
// streams are included only when full is set.
func movieItem(m *libraryv1.Movie, full bool) Item {
	id := jellyfinID(m.GetId())
	createdAt := m.GetCreatedAt().AsTime()
	source, width, height, hasSubtitles := videoFields(id, m.GetPath(), m.GetMediaInfo(), externalSubtitles(m.GetExternalSubtitles()))

	var sources []MediaSource
	var streams []MediaStream
	if full {
		sources = []MediaSource{source}
		streams = source.MediaStreams
	}

	return Item{
		Name:                     m.GetTitle(),
		ServerId:                 ServerID,
		Id:                       id,
		DateCreated:              &createdAt,
		CanDelete:                false,
		CanDownload:              false,
		HasSubtitles:             hasSubtitles,
		Container:                source.Container,
		SortName:                 m.GetSortTitle(),
		ExternalUrls:             []any{},
		MediaSources:             sources,
		Path:                     m.GetPath(),
		EnableMediaSourceDisplay: true,
		Taglines:                 []string{},
		Genres:                   []string{},
		RunTimeTicks:             source.RunTimeTicks,
		PlayAccess:               "Full",
		ProductionYear:           int(m.GetYear()),
		RemoteTrailers:           []any{},
		ProviderIds:              map[string]string{},
		IsHD:                     height >= 720,
		IsFolder:                 false,
		ParentId:                 jellyfinID(m.GetLibraryId()),
		Type:                     "Movie",
		People:                   []any{},
		Studios:                  []any{},
		GenreItems:               []any{},
		LocalTrailerCount:        0,
		UserData:                 UserItemData{Key: id, ItemId: id},
		SpecialFeatureCount:      0,
		DisplayPreferencesId:     id,
		Tags:                     []string{},
		MediaStreams:             streams,
		VideoType:                "VideoFile",
		ImageTags:                map[string]string{},
		BackdropImageTags:        []string{},
		ImageBlurHashes:          map[string]map[string]string{},
		LocationType:             "FileSystem",
		MediaType:                "Video",
		LockedFields:             []string{},
		LockData:                 false,
		Width:                    width,
		Height:                   height,
		Chapters:                 []any{},
	}
}

// seriesItem maps a series to a Jellyfin series: a folder of seasons.
func seriesItem(s *libraryv1.Series) Item {
	id := jellyfinID(s.GetId())
	createdAt := s.GetCreatedAt().AsTime()
	seasons, episodes := int(s.GetSeasonCount()), int(s.GetEpisodeCount())
	return Item{
		Name:                     s.GetTitle(),
		ServerId:                 ServerID,
		Id:                       id,
		DateCreated:              &createdAt,
		CanDelete:                false,
		CanDownload:              false,
		SortName:                 s.GetSortTitle(),
		ExternalUrls:             []any{},
		EnableMediaSourceDisplay: true,
		Taglines:                 []string{},
		Genres:                   []string{},
		PlayAccess:               "Full",
		ProductionYear:           int(s.GetYear()),
		RemoteTrailers:           []any{},
		ProviderIds:              map[string]string{},
		IsFolder:                 true,
		ParentId:                 jellyfinID(s.GetLibraryId()),
		Type:                     "Series",
		People:                   []any{},
		Studios:                  []any{},
		GenreItems:               []any{},
		UserData:                 UserItemData{Key: id, ItemId: id, UnplayedItemCount: &episodes},
		DisplayPreferencesId:     id,
		Tags:                     []string{},
		ChildCount:               &seasons,
		RecursiveItemCount:       &episodes,
		ImageTags:                map[string]string{},
		BackdropImageTags:        []string{},
		ImageBlurHashes:          map[string]map[string]string{},
		LocationType:             "FileSystem",
		MediaType:                "Unknown",
		LockedFields:             []string{},
		Chapters:                 []any{},
	}
}

// seasonItem maps a season to a Jellyfin season: a folder of episodes.
func seasonItem(s *libraryv1.Season) Item {
	id := jellyfinID(s.GetId())
	createdAt := s.GetCreatedAt().AsTime()
	number, episodes := int(s.GetNumber()), int(s.GetEpisodeCount())
	return Item{
		Name:                     seasonName(number),
		ServerId:                 ServerID,
		Id:                       id,
		DateCreated:              &createdAt,
		CanDelete:                false,
		CanDownload:              false,
		SortName:                 fmt.Sprintf("%04d", number),
		ExternalUrls:             []any{},
		EnableMediaSourceDisplay: true,
		Taglines:                 []string{},
		Genres:                   []string{},
		PlayAccess:               "Full",
		ProductionYear:           int(s.GetSeriesYear()),
		IndexNumber:              &number,
		RemoteTrailers:           []any{},
		ProviderIds:              map[string]string{},
		IsFolder:                 true,
		ParentId:                 jellyfinID(s.GetSeriesId()),
		Type:                     "Season",
		People:                   []any{},
		Studios:                  []any{},
		GenreItems:               []any{},
		UserData:                 UserItemData{Key: id, ItemId: id, UnplayedItemCount: &episodes},
		DisplayPreferencesId:     id,
		Tags:                     []string{},
		ChildCount:               &episodes,
		RecursiveItemCount:       &episodes,
		SeriesId:                 jellyfinID(s.GetSeriesId()),
		SeriesName:               s.GetSeriesTitle(),
		ImageTags:                map[string]string{},
		BackdropImageTags:        []string{},
		ImageBlurHashes:          map[string]map[string]string{},
		LocationType:             "FileSystem",
		MediaType:                "Unknown",
		LockedFields:             []string{},
		Chapters:                 []any{},
	}
}

// episodeItem maps an episode to a Jellyfin episode. The media sources
// and streams are included only when full is set.
func episodeItem(e *libraryv1.Episode, full bool) Item {
	id := jellyfinID(e.GetId())
	createdAt := e.GetCreatedAt().AsTime()
	source, width, height, hasSubtitles := videoFields(id, e.GetPath(), e.GetMediaInfo(), externalSubtitles(e.GetExternalSubtitles()))
	season := int(e.GetSeasonNumber())

	var indexNumber, indexNumberEnd *int
	if e.Number != nil {
		number := int(e.GetNumber())
		indexNumber = &number
	}
	if e.EndNumber != nil {
		end := int(e.GetEndNumber())
		indexNumberEnd = &end
	}

	var sources []MediaSource
	var streams []MediaStream
	if full {
		sources = []MediaSource{source}
		streams = source.MediaStreams
	}

	return Item{
		Name:                     e.GetTitle(),
		ServerId:                 ServerID,
		Id:                       id,
		DateCreated:              &createdAt,
		CanDelete:                false,
		CanDownload:              false,
		HasSubtitles:             hasSubtitles,
		Container:                source.Container,
		SortName:                 episodeSortName(e),
		ExternalUrls:             []any{},
		MediaSources:             sources,
		Path:                     e.GetPath(),
		EnableMediaSourceDisplay: true,
		Taglines:                 []string{},
		Genres:                   []string{},
		RunTimeTicks:             source.RunTimeTicks,
		PlayAccess:               "Full",
		ProductionYear:           int(e.GetSeriesYear()),
		IndexNumber:              indexNumber,
		IndexNumberEnd:           indexNumberEnd,
		ParentIndexNumber:        &season,
		RemoteTrailers:           []any{},
		ProviderIds:              map[string]string{},
		IsHD:                     height >= 720,
		IsFolder:                 false,
		ParentId:                 jellyfinID(e.GetSeasonId()),
		Type:                     "Episode",
		People:                   []any{},
		Studios:                  []any{},
		GenreItems:               []any{},
		LocalTrailerCount:        0,
		UserData:                 UserItemData{Key: id, ItemId: id},
		SpecialFeatureCount:      0,
		DisplayPreferencesId:     id,
		Tags:                     []string{},
		SeriesName:               e.GetSeriesTitle(),
		SeriesId:                 jellyfinID(e.GetSeriesId()),
		SeasonId:                 jellyfinID(e.GetSeasonId()),
		SeasonName:               seasonName(season),
		MediaStreams:             streams,
		VideoType:                "VideoFile",
		ImageTags:                map[string]string{},
		BackdropImageTags:        []string{},
		ImageBlurHashes:          map[string]map[string]string{},
		LocationType:             "FileSystem",
		MediaType:                "Video",
		LockedFields:             []string{},
		LockData:                 false,
		Width:                    width,
		Height:                   height,
		Chapters:                 []any{},
	}
}

// videoFields builds the media source of a video file and returns the
// width, the height and whether it has subtitles.
func videoFields(id, filePath string, info *libraryv1.MediaInfo, subs []ExternalSubtitle) (MediaSource, int, int, bool) {
	streams := []MediaStream{}
	var (
		width, height     int
		hasSubtitles      bool
		defaultAudioIndex *int
		defaultAudio      bool
		runTimeTicks      int64
		size, bitrate     int64
	)
	if info != nil {
		runTimeTicks = info.GetDurationMs() * ticksPerMs
		size = info.GetSize()
		bitrate = info.GetBitRate()
		for _, s := range info.GetStreams() {
			// Only external subtitles are shown: the ones embedded in the
			// video file are skipped.
			if s.GetType() == "subtitle" {
				continue
			}
			ms := mediaStream(s)
			streams = append(streams, ms)
			switch s.GetType() {
			case "video":
				if width == 0 {
					width, height = int(s.GetWidth()), int(s.GetHeight())
				}
			case "audio":
				if defaultAudioIndex == nil || (s.GetDefault() && !defaultAudio) {
					index := ms.Index
					defaultAudioIndex = &index
					defaultAudio = s.GetDefault()
				}
			}
		}
	}
	base := externalSubtitleBase(info)
	for i, s := range subs {
		streams = append(streams, externalSubtitleStream(id, base+i, s))
		hasSubtitles = true
	}

	source := MediaSource{
		Protocol:                "File",
		Id:                      id,
		Path:                    filePath,
		Type:                    "Default",
		Container:               container(filePath),
		Size:                    size,
		Name:                    strings.TrimSuffix(path.Base(filePath), path.Ext(filePath)),
		IsRemote:                false,
		RunTimeTicks:            runTimeTicks,
		ReadAtNativeFramerate:   false,
		IgnoreDts:               false,
		IgnoreIndex:             false,
		GenPtsInput:             false,
		SupportsTranscoding:     false,
		SupportsDirectStream:    true,
		SupportsDirectPlay:      true,
		IsInfiniteStream:        false,
		RequiresOpening:         false,
		RequiresClosing:         false,
		RequiresLooping:         false,
		SupportsProbing:         true,
		VideoType:               "VideoFile",
		MediaStreams:            streams,
		MediaAttachments:        []any{},
		Formats:                 []string{},
		Bitrate:                 bitrate,
		DefaultAudioStreamIndex: defaultAudioIndex,
		HasSegments:             false,
	}
	return source, width, height, hasSubtitles
}

// externalSubtitles converts the external subtitles of a library item.
func externalSubtitles(subs []*libraryv1.ExternalSubtitle) []ExternalSubtitle {
	out := make([]ExternalSubtitle, len(subs))
	for i, s := range subs {
		out[i] = ExternalSubtitle{
			ID:              s.GetId(),
			FileID:          s.GetFileId(),
			Format:          s.GetFormat(),
			Language:        s.GetLanguage(),
			Title:           s.GetTitle(),
			Forced:          s.GetForced(),
			HearingImpaired: s.GetHearingImpaired(),
		}
	}
	return out
}

// externalSubtitleBase is the index of the first external subtitle stream
// of a media source: the ones after the streams shown by the plugin. The
// subtitles embedded in the file are not shown, so their indices are free.
func externalSubtitleBase(info *libraryv1.MediaInfo) int {
	base := 0
	for _, s := range info.GetStreams() {
		if s.GetType() == "subtitle" {
			continue
		}
		base = max(base, int(s.GetIndex())+1)
	}
	return base
}

// externalSubtitleStream maps a subtitle file beside the video to a
// subtitle stream, delivered by the plugin. itemID is the ID of the item,
// which is also the ID of its only media source.
func externalSubtitleStream(itemID string, index int, s ExternalSubtitle) MediaStream {
	codec := strings.ToLower(s.Format)
	language, name := subtitleLanguage(s.Language)

	parts := []string{}
	if name != "" {
		parts = append(parts, name)
	}
	if s.Title != "" {
		parts = append(parts, s.Title)
	}
	if len(parts) == 0 {
		parts = append(parts, "Unknown")
	}
	parts = append(parts, strings.ToUpper(codec))
	if s.Forced {
		parts = append(parts, "Forced")
	}
	if s.HearingImpaired {
		parts = append(parts, "SDH")
	}
	parts = append(parts, "External")

	return MediaStream{
		Codec:                  codec,
		Language:               language,
		AudioSpatialFormat:     "None",
		DisplayTitle:           strings.Join(parts, " - "),
		Title:                  s.Title,
		IsForced:               s.Forced,
		IsHearingImpaired:      s.HearingImpaired,
		Type:                   "Subtitle",
		Index:                  index,
		IsExternal:             true,
		IsTextSubtitleStream:   true,
		SupportsExternalStream: true,
		DeliveryMethod:         "External",
		DeliveryUrl:            fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.%s", itemID, itemID, index, codec),
	}
}

func mediaStream(s *libraryv1.Stream) MediaStream {
	typ, videoRange, rangeType, text := streamKind(s)
	return MediaStream{
		Codec:                  s.GetCodec(),
		Language:               s.GetLanguage(),
		ColorTransfer:          s.GetColorTransfer(),
		ColorPrimaries:         s.GetColorPrimaries(),
		ColorSpace:             s.GetColorSpace(),
		VideoRange:             videoRange,
		VideoRangeType:         rangeType,
		AudioSpatialFormat:     "None",
		DisplayTitle:           displayTitle(s, videoRange),
		Title:                  s.GetTitle(),
		IsInterlaced:           s.GetInterlaced(),
		BitRate:                s.GetBitRate(),
		BitDepth:               int(s.GetBitDepth()),
		ChannelLayout:          s.GetChannelLayout(),
		Channels:               int(s.GetChannels()),
		SampleRate:             int(s.GetSampleRate()),
		IsDefault:              s.GetDefault(),
		IsForced:               s.GetForced(),
		IsHearingImpaired:      s.GetHearingImpaired(),
		Height:                 int(s.GetHeight()),
		Width:                  int(s.GetWidth()),
		AverageFrameRate:       s.GetFrameRate(),
		RealFrameRate:          s.GetFrameRate(),
		Profile:                s.GetProfile(),
		Type:                   typ,
		AspectRatio:            s.GetAspectRatio(),
		Index:                  int(s.GetIndex()),
		IsTextSubtitleStream:   text,
		SupportsExternalStream: text,
		PixelFormat:            s.GetPixelFormat(),
		Level:                  int(s.GetLevel()),
	}
}

// streamKind classifies a stream for Jellyfin.
func streamKind(s *libraryv1.Stream) (typ, videoRange, rangeType string, text bool) {
	switch s.GetType() {
	case "video":
		videoRange, rangeType = videoRangeOf(s.GetColorTransfer())
		return "Video", videoRange, rangeType, false
	case "audio":
		return "Audio", "", "", false
	case "subtitle":
		text = textSubtitleCodecs[s.GetCodec()]
		return "Subtitle", "", "", text
	default:
		return "Data", "", "", false
	}
}

var textSubtitleCodecs = map[string]bool{
	"ass": true, "mov_text": true, "srt": true, "ssa": true, "subrip": true,
	"text": true, "ttml": true, "webvtt": true,
}

// videoRangeOf classifies the dynamic range of a video by its transfer
// characteristics. Dolby Vision is not detected.
func videoRangeOf(transfer string) (videoRange, rangeType string) {
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

// container is the container of a file the way Jellyfin names it: its
// extension.
func container(p string) string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(p)), ".")
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

// User maps the authenticated caller to a Jellyfin user: a Jellyfin user is
// a profile, and its id is the profile id.
func (s *Service) User(c Caller) User {
	return user(c.AccountDisabled, c.ProfileID, c.ProfileDisplayName)
}

func user(disabled bool, profileID, displayName string) User {
	return User{
		Name:                      displayName,
		ServerId:                  ServerID,
		ServerName:                ServerName,
		Id:                        jellyfinID(profileID),
		HasPassword:               true,
		HasConfiguredPassword:     true,
		HasConfiguredEasyPassword: false,
		EnableAutoLogin:           false,
		Configuration: UserConfiguration{
			PlayDefaultAudioTrack:      true,
			GroupedFolders:             []string{},
			SubtitleMode:               "Default",
			OrderedViews:               []string{},
			LatestItemsExcludes:        []string{},
			MyMediaExcludes:            []string{},
			HidePlayedInLatest:         true,
			RememberAudioSelections:    true,
			RememberSubtitleSelections: true,
			EnableNextEpisodeAutoPlay:  true,
		},
		Policy: UserPolicy{
			// To Jellyfin everyone isn't admin.
			IsAdministrator:                  false,
			IsDisabled:                       disabled,
			BlockedTags:                      []string{},
			AllowedTags:                      []string{},
			EnableUserPreferenceAccess:       true,
			AccessSchedules:                  []any{},
			BlockUnratedItems:                []string{},
			EnableRemoteAccess:               true,
			EnableMediaPlayback:              true,
			EnableAudioPlaybackTranscoding:   true,
			EnableVideoPlaybackTranscoding:   true,
			EnablePlaybackRemuxing:           true,
			EnableContentDeletionFromFolders: []string{},
			EnableContentDownloading:         true,
			EnabledDevices:                   []string{},
			EnableAllDevices:                 true,
			EnabledChannels:                  []string{},
			EnableAllChannels:                true,
			EnabledFolders:                   []string{},
			EnableAllFolders:                 true,
			BlockedMediaFolders:              []string{},
			BlockedChannels:                  []string{},
			AuthenticationProviderId:         "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
			PasswordResetProviderId:          "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
			SyncPlayAccess:                   "CreateAndJoinGroups",
		},
	}
}

// SessionInfo describes a session. The device data comes from the
// authorization of the request.
func (s *Service) SessionInfo(c Caller, d Device) SessionInfo {
	return SessionInfo{
		Id:                    jellyfinID(c.SessionID),
		UserId:                jellyfinID(c.ProfileID),
		UserName:              c.ProfileDisplayName,
		Client:                d.Client,
		LastActivityDate:      time.Now().UTC(),
		DeviceName:            d.Name,
		DeviceId:              d.ID,
		ApplicationVersion:    d.Version,
		IsActive:              true,
		SupportsMediaControl:  false,
		SupportsRemoteControl: false,
		HasCustomDeviceName:   false,
		ServerId:              ServerID,
		AdditionalUsers:       []any{},
		PlayableMediaTypes:    []string{},
		SupportedCommands:     []string{},
	}
}
