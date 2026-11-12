package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	playbackv1 "github.com/lukerops/mserver/gen/playback/v1"
	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/internal/kv"
)

// playSessionTTL is how long the playback session of a PlaySessionId is
// remembered after its last report.
const playSessionTTL = time.Hour

// Kinds of playback reports.
const (
	reportStart    = "start"
	reportProgress = "progress"
	reportStop     = "stop"
)

// PlaybackInfo answers the media sources of a video, all of them played as
// they are (no transcoding). A source the device profile cannot play
// directly is an error "NoCompatibleStream". The PlaySessionId is new on
// every call: the session starts only when the client reports it is
// playing.
func (s *Service) PlaybackInfo(ctx context.Context, v Video, req PlaybackInfoRequest, queryAudioIndex *int) PlaybackInfo {
	audioIndex := req.AudioStreamIndex.intPtr()
	if audioIndex == nil {
		audioIndex = queryAudioIndex
	}
	applySubtitleProfile(&v.Item, req.DeviceProfile)

	res := PlaybackInfo{
		MediaSources:  []MediaSource{},
		PlaySessionId: jellyfinID(uuid.NewString()),
	}
	for _, source := range v.Item.MediaSources {
		attrs := []slog.Attr{
			slog.String("item_id", v.Item.Id),
			slog.String("source_id", source.Id),
			slog.String("container", source.Container),
		}
		if req.DeviceProfile == nil {
			s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback info: direct play",
				append(attrs, slog.String("reason", "no device profile"))...)
		} else {
			d := directPlay(req.DeviceProfile.DirectPlayProfiles, source, audioIndex)
			attrs = append(attrs,
				slog.String("video_codec", d.videoCodec),
				slog.String("audio_codec", d.audioCodec),
				slog.Bool("direct_playable", d.profile >= 0),
			)
			if d.profile < 0 {
				s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback info: source rejected",
					append(attrs,
						slog.String("reason", d.rejected),
						slog.Int("profiles", len(req.DeviceProfile.DirectPlayProfiles)),
					)...)
				continue
			}
			s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback info: direct play",
				append(attrs, slog.Int("profile", d.profile))...)
		}
		res.MediaSources = append(res.MediaSources, source)
	}
	if len(res.MediaSources) == 0 {
		res.ErrorCode = "NoCompatibleStream"
	}
	s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session created",
		slog.String("item_id", v.Item.Id),
		slog.String("play_session_id", res.PlaySessionId),
		slog.Int("sources", len(res.MediaSources)),
	)
	return res
}

// directPlayResult is the decision of directPlay for a media source.
type directPlayResult struct {
	// profile is the index of the first direct play profile that accepts
	// the source, or -1.
	profile                int
	videoCodec, audioCodec string
	// rejected tells why no profile accepts the source.
	rejected string
}

// directPlay finds the first video direct play profile that accepts the
// container, the video codec and the audio codec (of the audio stream
// asked, or the default one) of a source, and tells why none does. The
// bitrate is not checked. Unknown codecs (a file not analyzed yet) are
// accepted.
func directPlay(profiles []DirectPlayProfile, source MediaSource, audioIndex *int) directPlayResult {
	d := directPlayResult{profile: -1}
	if audioIndex == nil {
		audioIndex = source.DefaultAudioStreamIndex
	}
	for _, s := range source.MediaStreams {
		switch {
		case s.Type == "Video" && d.videoCodec == "":
			d.videoCodec = s.Codec
		case s.Type == "Audio" && audioIndex != nil && s.Index == *audioIndex:
			d.audioCodec = s.Codec
		}
	}
	// The furthest check a profile passes tells why the source is rejected.
	furthest := 0
	for i, dp := range profiles {
		passed := 0
		switch {
		case !strings.EqualFold(dp.Type, "Video"):
		case !listAccepts(dp.Container, source.Container):
			passed = 1
		case !listAccepts(dp.VideoCodec, d.videoCodec):
			passed = 2
		case !listAccepts(dp.AudioCodec, d.audioCodec):
			passed = 3
		default:
			d.profile = i
			return d
		}
		furthest = max(furthest, passed)
	}
	d.rejected = [...]string{
		"no video direct play profile",
		"container not supported",
		"video codec not supported",
		"audio codec not supported",
	}[furthest]
	return d
}

// listAccepts reports whether a comma-separated list of a profile accepts
// a value: an empty list or an unknown value always does.
func listAccepts(list, value string) bool {
	if strings.TrimSpace(list) == "" || value == "" {
		return true
	}
	for v := range strings.SplitSeq(list, ",") {
		if strings.EqualFold(strings.TrimSpace(v), value) {
			return true
		}
	}
	return false
}

// ResolveFile asks the storage for the URL of a file.
func (s *Service) ResolveFile(ctx context.Context, fileID string) (string, error) {
	if s.storage == nil {
		return "", errUnavailable("no storage module")
	}
	res, err := s.storage.ResolveFile(ctx, &storagev1.ResolveFileRequest{Id: fileID})
	switch {
	case err == nil:
		return res.GetUrl(), nil
	case ctx.Err() != nil:
		return "", ctx.Err()
	case isCode(err, connect.CodeNotFound):
		return "", errNotFound("file not found in storage")
	case isCode(err, connect.CodeUnavailable), isCode(err, connect.CodeFailedPrecondition):
		return "", errUnavailable("storage unavailable")
	default:
		return "", err
	}
}

// StartReport reports the start of a playback.
func (s *Service) StartReport(ctx context.Context, c Caller, r PlaybackReport) error {
	return s.reportPlayback(ctx, c, r, reportStart)
}

// ProgressReport reports the position of a playback.
func (s *Service) ProgressReport(ctx context.Context, c Caller, r PlaybackReport) error {
	return s.reportPlayback(ctx, c, r, reportProgress)
}

// StopReport reports the end of a playback.
func (s *Service) StopReport(ctx context.Context, c Caller, r PlaybackReport) error {
	return s.stopPlayback(ctx, c, r)
}

// Ping keeps the playback session of a play session alive.
func (s *Service) Ping(ctx context.Context, c Caller, playSessionID string) error {
	if s.playback == nil {
		return nil
	}
	key := playSessionKey(c, playSessionID)
	id, found, err := s.mappedSession(ctx, key)
	switch {
	case err != nil:
	case !found:
		s.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: report for unknown play session",
			slog.String("kind", "ping"), slog.String("play_session_id", playSessionID))
	default:
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback ping",
			slog.String("play_session_id", playSessionID), slog.String("playback_session_id", id))
		_, err = s.playback.HeartbeatSession(ctx, &playbackv1.HeartbeatSessionRequest{Id: id})
		if err == nil {
			err = s.renewMapping(ctx, key, id)
		} else if isCode(err, connect.CodeNotFound) {
			s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback session expired",
				slog.String("play_session_id", playSessionID), slog.String("playback_session_id", id))
		}
	}
	if err != nil && !isCode(err, connect.CodeNotFound) {
		return err
	}
	return nil
}

// playSessionKey is the kv key of the playback session of a play session
// of a profile. Clients that send no PlaySessionId get one per identity
// session.
func playSessionKey(c Caller, playSessionID string) string {
	if playSessionID == "" {
		playSessionID = "session-" + c.SessionID
	}
	return "playsessions/" + c.ProfileID + "/" + strings.ToLower(playSessionID)
}

// mappedSession returns the ID of the playback session of a play session,
// if any.
func (s *Service) mappedSession(ctx context.Context, key string) (string, bool, error) {
	value, err := s.kv.Get(ctx, key)
	if errors.Is(err, kv.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(value), true, nil
}

// renewMapping extends the life of a play session mapping. A mapping
// that expired meanwhile is created again.
func (s *Service) renewMapping(ctx context.Context, key, id string) error {
	err := s.kv.Update(ctx, key, []byte(id), playSessionTTL)
	if errors.Is(err, kv.ErrNotFound) {
		err = s.kv.Create(ctx, key, []byte(id), playSessionTTL)
		if errors.Is(err, kv.ErrExists) {
			return nil
		}
	}
	return err
}

func positionMs(report PlaybackReport) int64 {
	if report.PositionTicks == nil || *report.PositionTicks < 0 {
		return 0
	}
	return int64(*report.PositionTicks) / ticksPerMs
}

// reportPlayback reports the position of the playback session of a play
// session, starting one if there is none (or it was stopped).
func (s *Service) reportPlayback(ctx context.Context, c Caller, report PlaybackReport, kind string) error {
	if s.playback == nil {
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback report ignored",
			slog.String("reason", "no playback module"))
		return nil
	}
	key := playSessionKey(c, report.PlaySessionId)
	id, found, err := s.mappedSession(ctx, key)
	if err != nil {
		return err
	}
	if found {
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session found",
			slog.String("play_session_id", report.PlaySessionId), slog.String("playback_session_id", id))
		_, err := s.playback.ReportProgress(ctx, &playbackv1.ReportProgressRequest{
			Id:         id,
			PositionMs: positionMs(report),
			Paused:     report.IsPaused,
		})
		if !isCode(err, connect.CodeNotFound) {
			if err != nil {
				return err
			}
			return s.renewMapping(ctx, key, id)
		}
		// The playback session timed out: start another one.
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback session expired",
			slog.String("play_session_id", report.PlaySessionId), slog.String("playback_session_id", id))
	} else if kind != reportStart {
		s.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: report for unknown play session",
			slog.String("kind", kind),
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("item_id", report.ItemId),
		)
	}

	sess, err := s.startPlayback(ctx, c, report)
	if err != nil || sess == nil {
		return err
	}
	if found {
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session mapped",
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("playback_session_id", sess.GetId()),
			slog.String("replaces", id),
		)
		return s.renewMapping(ctx, key, sess.GetId())
	}
	err = s.kv.Create(ctx, key, []byte(sess.GetId()), playSessionTTL)
	if errors.Is(err, kv.ErrExists) {
		// A concurrent report of the same play session started its own
		// playback session first: keep that one.
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session mapped concurrently",
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("dropped_playback_session_id", sess.GetId()),
		)
		if _, err := s.playback.StopSession(ctx, &playbackv1.StopSessionRequest{Id: sess.GetId()}); err != nil &&
			!isCode(err, connect.CodeNotFound) {
			return err
		}
		return s.reportPlayback(ctx, c, report, kind)
	}
	if err == nil {
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session mapped",
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("playback_session_id", sess.GetId()),
		)
	}
	return err
}

// startPlayback starts a playback session for the item of a report. It
// returns nil if the item is not a movie or an episode.
func (s *Service) startPlayback(ctx context.Context, c Caller, report PlaybackReport) (*playbackv1.Session, error) {
	ignored := func(reason string) (*playbackv1.Session, error) {
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback report ignored",
			slog.String("reason", reason),
			slog.String("item_id", report.ItemId),
			slog.String("play_session_id", report.PlaySessionId),
		)
		return nil, nil
	}
	itemID, err := uuid.Parse(report.ItemId)
	if err != nil {
		return ignored("invalid ItemId")
	}
	if s.library == nil {
		return ignored("no library module")
	}
	ref, err := s.findItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	var fileID string
	switch {
	case ref.movie != nil:
		fileID = ref.movie.GetFileId()
	case ref.episode != nil:
		fileID = ref.episode.GetFileId()
	default:
		return ignored("item is not a movie or an episode")
	}
	res, err := s.playback.StartSession(ctx, &playbackv1.StartSessionRequest{
		AccountId: c.AccountID,
		ProfileId: c.ProfileID,
		DeviceId:  c.DeviceID,
		ItemId:    itemID.String(),
		FileId:    fileID,
		// Every source is served as is: direct play and direct stream
		// (the file through /Videos/{id}/stream) are both direct play.
		PlayMethod: "direct_play",
		PositionMs: positionMs(report),
		Paused:     report.IsPaused,
	})
	if err != nil {
		return nil, err
	}
	return res.GetSession(), nil
}

// stopPlayback stops the playback session of a play session, if any.
func (s *Service) stopPlayback(ctx context.Context, c Caller, report PlaybackReport) error {
	if s.playback == nil {
		s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback report ignored",
			slog.String("reason", "no playback module"))
		return nil
	}
	key := playSessionKey(c, report.PlaySessionId)
	id, found, err := s.mappedSession(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		s.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: report for unknown play session",
			slog.String("kind", reportStop),
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("item_id", report.ItemId),
		)
		return nil
	}
	s.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session stopped",
		slog.String("play_session_id", report.PlaySessionId), slog.String("playback_session_id", id))
	req := &playbackv1.StopSessionRequest{Id: id}
	if report.PositionTicks != nil {
		pos := positionMs(report)
		req.PositionMs = &pos
	}
	if _, err := s.playback.StopSession(ctx, req); err != nil && !isCode(err, connect.CodeNotFound) {
		return err
	}
	if err := s.kv.Delete(ctx, key); err != nil && !errors.Is(err, kv.ErrNotFound) {
		return err
	}
	return nil
}
