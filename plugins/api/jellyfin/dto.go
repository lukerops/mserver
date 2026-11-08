package jellyfin

import (
	"strings"
	"time"

	identityrouter "github.com/lukerops/mserver/internal/modules/identity/router"
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

// userDto maps a profile to a Jellyfin user: a Jellyfin user is a profile,
// and its id is the profile id.
func (p *plugin) userDto(account identityrouter.AccountV1, profile identityrouter.ProfileV1) UserDto {
	return UserDto{
		Name:                  profile.DisplayName,
		ServerId:              p.serverID,
		ServerName:            serverName,
		Id:                    jellyfinID(profile.ID.String()),
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
			IsAdministrator:                  account.IsAdmin,
			IsDisabled:                       account.Disabled,
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
func (p *plugin) sessionInfoDto(session identityrouter.SessionV1, profile identityrouter.ProfileV1, a authorization) SessionInfoDto {
	return SessionInfoDto{
		Id:                 jellyfinID(session.ID.String()),
		UserId:             jellyfinID(profile.ID.String()),
		UserName:           profile.DisplayName,
		Client:             a.Client,
		LastActivityDate:   time.Now().UTC(),
		DeviceName:         a.Device,
		DeviceId:           a.DeviceID,
		ApplicationVersion: a.Version,
		IsActive:           true,
		ServerId:           p.serverID,
		AdditionalUsers:    []any{},
		PlayableMediaTypes: []string{},
		SupportedCommands:  []string{},
	}
}

// jellyfinID formats a UUID the way Jellyfin does: without hyphens.
func jellyfinID(uuid string) string {
	return strings.ReplaceAll(uuid, "-", "")
}
