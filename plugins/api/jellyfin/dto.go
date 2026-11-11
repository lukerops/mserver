package jellyfin

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
)

// The DTOs follow the schemas of the Jellyfin 12.4.0 OpenAPI. Field names
// are the JSON names (PascalCase), so they carry no tags. Arrays are
// always initialized: clients do not expect null.

type PublicSystemInfo struct {
	LocalAddress           string
	ServerName             string
	Version                string
	ProductName            string
	OperatingSystem        string
	Id                     string
	StartupWizardCompleted bool
}

type SystemInfo struct {
	PublicSystemInfo
	OperatingSystemDisplayName string
	PackageName                string
	HasPendingRestart          bool
	IsShuttingDown             bool
	SupportsLibraryMonitor     bool
	WebSocketPortNumber        int
	CompletedInstallations     []any
	CanSelfRestart             bool
	CanLaunchWebBrowser        bool
	HasUpdateAvailable         bool
	SystemArchitecture         string
	CastReceiverApplications   []any
}

type BrandingOptionsDto struct {
	LoginDisclaimer     string
	CustomCss           string
	SplashscreenEnabled bool
}

type EndPointInfo struct {
	IsLocal     bool
	IsInNetwork bool
}

type AuthenticateUserByName struct {
	Username string
	Pw       string
}

type UserConfiguration struct {
	AudioLanguagePreference    string
	PlayDefaultAudioTrack      bool
	SubtitleLanguagePreference string
	DisplayMissingEpisodes     bool
	GroupedFolders             []string
	SubtitleMode               string
	DisplayCollectionsView     bool
	EnableLocalPassword        bool
	OrderedViews               []string
	LatestItemsExcludes        []string
	MyMediaExcludes            []string
	HidePlayedInLatest         bool
	RememberAudioSelections    bool
	RememberSubtitleSelections bool
	EnableNextEpisodeAutoPlay  bool
}

type UserPolicy struct {
	IsAdministrator                  bool
	IsHidden                         bool
	EnableCollectionManagement       bool
	EnableSubtitleManagement         bool
	EnableLyricManagement            bool
	IsDisabled                       bool
	BlockedTags                      []string
	AllowedTags                      []string
	EnableUserPreferenceAccess       bool
	AccessSchedules                  []any
	BlockUnratedItems                []string
	EnableRemoteControlOfOtherUsers  bool
	EnableSharedDeviceControl        bool
	EnableRemoteAccess               bool
	EnableLiveTvManagement           bool
	EnableLiveTvAccess               bool
	EnableMediaPlayback              bool
	EnableAudioPlaybackTranscoding   bool
	EnableVideoPlaybackTranscoding   bool
	EnablePlaybackRemuxing           bool
	ForceRemoteSourceTranscoding     bool
	EnableContentDeletion            bool
	EnableContentDeletionFromFolders []string
	EnableContentDownloading         bool
	EnableSyncTranscoding            bool
	EnableMediaConversion            bool
	EnabledDevices                   []string
	EnableAllDevices                 bool
	EnabledChannels                  []string
	EnableAllChannels                bool
	EnabledFolders                   []string
	EnableAllFolders                 bool
	InvalidLoginAttemptCount         int
	LoginAttemptsBeforeLockout       int
	MaxActiveSessions                int
	EnablePublicSharing              bool
	BlockedMediaFolders              []string
	BlockedChannels                  []string
	RemoteClientBitrateLimit         int
	AuthenticationProviderId         string
	PasswordResetProviderId          string
	SyncPlayAccess                   string
}

type UserDto struct {
	Name                      string
	ServerId                  string
	ServerName                string
	Id                        string
	HasPassword               bool
	HasConfiguredPassword     bool
	HasConfiguredEasyPassword bool
	EnableAutoLogin           bool
	LastLoginDate             *time.Time `json:",omitempty"`
	LastActivityDate          *time.Time `json:",omitempty"`
	Configuration             UserConfiguration
	Policy                    UserPolicy
}

type SessionInfoDto struct {
	Id                    string
	UserId                string
	UserName              string
	Client                string
	LastActivityDate      time.Time
	DeviceName            string
	DeviceId              string
	ApplicationVersion    string
	IsActive              bool
	SupportsMediaControl  bool
	SupportsRemoteControl bool
	HasCustomDeviceName   bool
	ServerId              string
	AdditionalUsers       []any
	PlayableMediaTypes    []string
	SupportedCommands     []string
}

type AuthenticationResult struct {
	User        UserDto
	SessionInfo SessionInfoDto
	AccessToken string
	ServerId    string
}

type DisplayPreferencesDto struct {
	Id                 string
	SortBy             string
	RememberIndexing   bool
	PrimaryImageHeight int
	PrimaryImageWidth  int
	CustomPrefs        map[string]string
	ScrollDirection    string
	ShowBackdrop       bool
	RememberSorting    bool
	SortOrder          string
	ShowSidebar        bool
	Client             string
}

type BaseItemDtoQueryResult struct {
	Items            []any
	TotalRecordCount int
	StartIndex       int
}

// ThemeMediaResult lists the theme songs or videos of an item.
type ThemeMediaResult struct {
	Items            []BaseItemDto
	TotalRecordCount int
	StartIndex       int
	OwnerId          string
}

type AllThemeMediaResult struct {
	ThemeVideosResult     ThemeMediaResult
	ThemeSongsResult      ThemeMediaResult
	SoundtrackSongsResult ThemeMediaResult
}

// QueryFiltersLegacy lists the values the items of a query can be
// filtered by (/Items/Filters).
type QueryFiltersLegacy struct {
	Genres          []string
	Tags            []string
	OfficialRatings []string
	Years           []int
}

// QueryFilters is the newer QueryFiltersLegacy (/Items/Filters2).
type QueryFilters struct {
	Genres            []NameGuidPair
	Tags              []string
	AudioLanguages    []NameValuePair
	SubtitleLanguages []NameValuePair
}

type NameGuidPair struct {
	Name string
	Id   string
}

type NameValuePair struct {
	Name  string
	Value string
}

// BaseItemDto describes a library (Type "CollectionFolder"), a movie
// ("Movie"), a series ("Series"), a season ("Season") or an episode
// ("Episode"). Optional fields are omitted when unknown.
type BaseItemDto struct {
	Name                     string
	ServerId                 string
	Id                       string
	DateCreated              *time.Time `json:",omitempty"`
	CanDelete                bool
	CanDownload              bool
	HasSubtitles             bool   `json:",omitempty"`
	Container                string `json:",omitempty"`
	SortName                 string
	ExternalUrls             []any
	MediaSources             []MediaSourceInfo `json:",omitempty"`
	Path                     string            `json:",omitempty"`
	EnableMediaSourceDisplay bool
	Taglines                 []string
	Genres                   []string
	RunTimeTicks             int64 `json:",omitempty"`
	PlayAccess               string
	ProductionYear           int `json:",omitempty"`
	// IndexNumber is the number of an episode (or season);
	// IndexNumberEnd the last episode of a file holding several;
	// ParentIndexNumber the season of an episode.
	IndexNumber          *int `json:",omitempty"`
	IndexNumberEnd       *int `json:",omitempty"`
	ParentIndexNumber    *int `json:",omitempty"`
	RemoteTrailers       []any
	ProviderIds          map[string]string
	IsHD                 bool `json:",omitempty"`
	IsFolder             bool
	ParentId             string `json:",omitempty"`
	Type                 string
	People               []any
	Studios              []any
	GenreItems           []any
	LocalTrailerCount    int
	UserData             UserItemDataDto
	SpecialFeatureCount  int
	DisplayPreferencesId string
	Tags                 []string
	// ChildCount is the number of seasons of a series, or of episodes of a
	// season; RecursiveItemCount the number of episodes of a series.
	ChildCount         *int          `json:",omitempty"`
	RecursiveItemCount *int          `json:",omitempty"`
	SeriesName         string        `json:",omitempty"`
	SeriesId           string        `json:",omitempty"`
	SeasonId           string        `json:",omitempty"`
	SeasonName         string        `json:",omitempty"`
	CollectionType     string        `json:",omitempty"`
	MediaStreams       []MediaStream `json:",omitempty"`
	VideoType          string        `json:",omitempty"`
	ImageTags          map[string]string
	BackdropImageTags  []string
	ImageBlurHashes    map[string]map[string]string
	LocationType       string
	MediaType          string
	LockedFields       []string
	LockData           bool
	Width              int `json:",omitempty"`
	Height             int `json:",omitempty"`
	Chapters           []any
}

// UserItemDataDto is the state of an item for the user. It is always
// empty until the watchstate module exists.
type UserItemDataDto struct {
	PlaybackPositionTicks int64
	PlayCount             int
	IsFavorite            bool
	Played                bool
	Key                   string
	ItemId                string
	// UnplayedItemCount is the number of episodes of a series or season
	// not played yet: all of them until watchstate.
	UnplayedItemCount *int `json:",omitempty"`
}

type MediaSourceInfo struct {
	Protocol                   string
	Id                         string
	Path                       string `json:",omitempty"`
	Type                       string
	Container                  string
	Size                       int64 `json:",omitempty"`
	Name                       string
	IsRemote                   bool
	RunTimeTicks               int64 `json:",omitempty"`
	ReadAtNativeFramerate      bool
	IgnoreDts                  bool
	IgnoreIndex                bool
	GenPtsInput                bool
	SupportsTranscoding        bool
	SupportsDirectStream       bool
	SupportsDirectPlay         bool
	IsInfiniteStream           bool
	RequiresOpening            bool
	RequiresClosing            bool
	RequiresLooping            bool
	SupportsProbing            bool
	VideoType                  string
	MediaStreams               []MediaStream
	MediaAttachments           []any
	Formats                    []string
	Bitrate                    int64 `json:",omitempty"`
	DefaultAudioStreamIndex    *int  `json:",omitempty"`
	DefaultSubtitleStreamIndex *int  `json:",omitempty"`
	HasSegments                bool
}

type MediaStream struct {
	Codec                  string
	Language               string `json:",omitempty"`
	ColorTransfer          string `json:",omitempty"`
	ColorPrimaries         string `json:",omitempty"`
	ColorSpace             string `json:",omitempty"`
	VideoRange             string `json:",omitempty"`
	VideoRangeType         string `json:",omitempty"`
	AudioSpatialFormat     string
	DisplayTitle           string
	Title                  string `json:",omitempty"`
	IsInterlaced           bool
	BitRate                int64  `json:",omitempty"`
	BitDepth               int    `json:",omitempty"`
	ChannelLayout          string `json:",omitempty"`
	Channels               int    `json:",omitempty"`
	SampleRate             int    `json:",omitempty"`
	IsDefault              bool
	IsForced               bool
	IsHearingImpaired      bool
	Height                 int     `json:",omitempty"`
	Width                  int     `json:",omitempty"`
	AverageFrameRate       float64 `json:",omitempty"`
	RealFrameRate          float64 `json:",omitempty"`
	Profile                string  `json:",omitempty"`
	Type                   string
	AspectRatio            string `json:",omitempty"`
	Index                  int
	IsExternal             bool
	IsTextSubtitleStream   bool
	SupportsExternalStream bool
	PixelFormat            string `json:",omitempty"`
	Level                  int
}

// PlaybackInfoDto is the body of POST /Items/{itemId}/PlaybackInfo. The
// same fields may come as query parameters; the body wins.
type PlaybackInfoDto struct {
	UserId              string
	StartTimeTicks      *flexInt
	AudioStreamIndex    *flexInt
	SubtitleStreamIndex *flexInt
	MediaSourceId       string
	DeviceProfile       *DeviceProfile
}

// flexInt is an integer of a request body that may come as a JSON number
// or as a string: jellyfin-web sends, for example, "AudioStreamIndex":"1"
// (the Jellyfin server reads numbers from strings).
type flexInt int64

func (n *flexInt) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		b = []byte(s)
	}
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return err
	}
	*n = flexInt(v)
	return nil
}

// intPtr converts an optional flexInt to an optional int.
func (n *flexInt) intPtr() *int {
	if n == nil {
		return nil
	}
	v := int(*n)
	return &v
}

// DeviceProfile describes what a client plays. Only the direct play
// profiles are used.
type DeviceProfile struct {
	DirectPlayProfiles []DirectPlayProfile
}

// DirectPlayProfile is a combination of container and codecs a client
// plays as is. The lists are comma-separated; an empty one accepts
// anything.
type DirectPlayProfile struct {
	Container  string
	AudioCodec string
	VideoCodec string
	// Type is "Audio", "Video" or "Photo".
	Type string
}

type PlaybackInfoResponse struct {
	MediaSources  []MediaSourceInfo
	PlaySessionId string
	// ErrorCode is "NoCompatibleStream" when no media source plays on
	// the client.
	ErrorCode string `json:",omitempty"`
}

// PlaybackReport is the body of the /Sessions/Playing reports
// (PlaybackStartInfo, PlaybackProgressInfo and PlaybackStopInfo), with
// the fields that are used.
type PlaybackReport struct {
	ItemId        string
	MediaSourceId string
	PlaySessionId string
	PositionTicks *flexInt
	IsPaused      bool
	// PlayMethod is "DirectPlay", "DirectStream" or "Transcode".
	PlayMethod string
}

type UtcTimeResponse struct {
	RequestReceptionTime     time.Time
	ResponseTransmissionTime time.Time
}

// userDto maps a profile to a Jellyfin user: a Jellyfin user is a profile,
// and its id is the profile id.
func (p *plugin) userDto(account *identityv1.Account, profile *identityv1.Profile) UserDto {
	return UserDto{
		Name:                  profile.GetDisplayName(),
		ServerId:              serverID,
		ServerName:            serverName,
		Id:                    jellyfinID(profile.GetId()),
		HasPassword:           true,
		HasConfiguredPassword: true,
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
			// To Jellyfin evereone isn't admin
			IsAdministrator:                  false,
			IsDisabled:                       account.GetDisabled(),
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

// sessionInfoDto describes a session. The device data comes from the
// authorization of the request.
func (p *plugin) sessionInfoDto(session *identityv1.Session, profile *identityv1.Profile, a authorization) SessionInfoDto {
	return SessionInfoDto{
		Id:                 jellyfinID(session.GetId()),
		UserId:             jellyfinID(profile.GetId()),
		UserName:           profile.GetDisplayName(),
		Client:             a.Client,
		LastActivityDate:   time.Now().UTC(),
		DeviceName:         a.Device,
		DeviceId:           a.DeviceID,
		ApplicationVersion: a.Version,
		IsActive:           true,
		ServerId:           serverID,
		AdditionalUsers:    []any{},
		PlayableMediaTypes: []string{},
		SupportedCommands:  []string{},
	}
}

// jellyfinID formats a UUID the way Jellyfin does: without hyphens.
func jellyfinID(uuid string) string {
	return strings.ReplaceAll(uuid, "-", "")
}
