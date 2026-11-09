package jellyfin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/kv"
	"github.com/lukerops/mserver/internal/infra/logging"

	playback "github.com/lukerops/mserver/internal/modules/playback/client"
	playbackrouter "github.com/lukerops/mserver/internal/modules/playback/router"
	storage "github.com/lukerops/mserver/internal/modules/storage/client"
)

// playSessionTTL is how long the playback session of a PlaySessionId is
// remembered after its last report.
const playSessionTTL = time.Hour

// video is a playable item: a movie or an episode.
type video struct {
	item       BaseItemDto
	locationID uuid.UUID
	fileID     uuid.UUID
}

// lookupVideo finds the movie or episode of the itemId path value. When
// there is none, it answers 404 and returns false.
func (p *plugin) lookupVideo(w http.ResponseWriter, r *http.Request) (video, bool) {
	ref, ok := p.lookupItem(w, r)
	if !ok {
		return video{}, false
	}
	switch {
	case ref.movie != nil:
		m := ref.movie
		return video{item: p.movieItem(*m, true), locationID: m.LocationID, fileID: m.FileID}, true
	case ref.episode != nil:
		e := ref.episode
		return video{item: p.episodeItem(*e, true), locationID: e.LocationID, fileID: e.FileID}, true
	}
	notFound(w, r, "item is not a movie or an episode")
	return video{}, false
}

// playbackInfo answers the media sources of a movie or episode, all of
// them played as they are (no transcoding). A source the device profile
// of the client cannot play directly is an error "NoCompatibleStream".
// The PlaySessionId is new on every call: the session starts only when
// the client reports it is playing.
func (p *plugin) playbackInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	var info PlaybackInfoDto
	if r.Method == http.MethodPost && !p.decodeBody(w, r, &info) {
		return
	}
	audioIndex := info.AudioStreamIndex.intPtr()
	if audioIndex == nil {
		if n, err := strconv.Atoi(lowerQuery(r)["audiostreamindex"]); err == nil {
			audioIndex = &n
		}
	}
	v, ok := p.lookupVideo(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	res := PlaybackInfoResponse{
		MediaSources:  []MediaSourceInfo{},
		PlaySessionId: jellyfinID(uuid.NewString()),
	}
	for _, source := range v.item.MediaSources {
		if info.DeviceProfile == nil {
			p.log.DebugContext(ctx, "jellyfin: playback info: direct play", "item_id", v.item.Id,
				"source_id", source.Id, "container", source.Container, "reason", "no device profile")
		} else {
			d := directPlay(info.DeviceProfile.DirectPlayProfiles, source, audioIndex)
			args := []any{"item_id", v.item.Id, "source_id", source.Id, "container", source.Container,
				"video_codec", d.videoCodec, "audio_codec", d.audioCodec, "direct_playable", d.profile >= 0}
			if d.profile < 0 {
				p.log.DebugContext(ctx, "jellyfin: playback info: source rejected",
					append(args, "reason", d.rejected, "profiles", len(info.DeviceProfile.DirectPlayProfiles))...)
				continue
			}
			p.log.DebugContext(ctx, "jellyfin: playback info: direct play", append(args, "profile", d.profile)...)
		}
		source.SupportsDirectPlay = true
		source.SupportsDirectStream = true
		source.SupportsTranscoding = false
		source.Protocol = "File"
		res.MediaSources = append(res.MediaSources, source)
	}
	if len(res.MediaSources) == 0 {
		res.ErrorCode = "NoCompatibleStream"
		logging.Annotate(ctx, "playback_error", res.ErrorCode)
	}
	p.log.DebugContext(ctx, "jellyfin: play session created", "item_id", v.item.Id,
		"play_session_id", res.PlaySessionId, "sources", len(res.MediaSources))
	writeJSON(w, http.StatusOK, res)
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
func directPlay(profiles []DirectPlayProfile, source MediaSourceInfo, audioIndex *int) directPlayResult {
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

// listAccepts reports whether a comma-separated list of a profile
// accepts a value: an empty list or an unknown value always does.
func listAccepts(list, value string) bool {
	if strings.TrimSpace(list) == "" || value == "" {
		return true
	}
	return contains(list, value)
}

// stream serves the file of a movie or episode as is, at
// /Videos/{itemId}/stream or /Videos/{itemId}/stream.{container}, with
// Range requests (seeking). The content comes from the storage.
func (p *plugin) stream(w http.ResponseWriter, r *http.Request) {
	if file := r.PathValue("file"); file != "stream" && !strings.HasPrefix(file, "stream.") {
		notFound(w, r, "unsupported stream file name")
		return
	}
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	v, ok := p.lookupVideo(w, r)
	if !ok {
		return
	}
	if p.storage == nil {
		unavailable(w, r, "no storage module")
		return
	}

	ctx := r.Context()
	p.log.DebugContext(ctx, "jellyfin: stream: storage request", "item_id", v.item.Id,
		"location_id", v.locationID, "file_id", v.fileID, "range", r.Header.Get("Range"))
	resp, err := p.storage.OpenFile(ctx, r.Method, v.locationID, v.fileID, r.Header)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		p.log.DebugContext(ctx, "jellyfin: stream: client went away", "error", err)
		logging.Annotate(ctx, "error", "client went away")
		return
	case errors.Is(err, storage.ErrNotFound):
		p.log.WarnContext(ctx, "jellyfin: stream: file not found in storage",
			"location_id", v.locationID, "file_id", v.fileID, "error", err)
		notFound(w, r, "file not found in storage")
		return
	case errors.Is(err, storage.ErrUnavailable):
		p.log.WarnContext(ctx, "jellyfin: stream: storage unavailable",
			"location_id", v.locationID, "file_id", v.fileID, "error", err)
		unavailable(w, r, "storage unavailable")
		return
	default:
		p.internalError(w, r, "open file", err)
		return
	}
	defer resp.Body.Close()
	p.log.DebugContext(ctx, "jellyfin: stream: storage response", "status", resp.StatusCode,
		"content_length", resp.ContentLength, "content_range", resp.Header.Get("Content-Range"))

	h := w.Header()
	for _, name := range []string{
		"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag",
	} {
		if v := resp.Header.Get(name); v != "" {
			h.Set(name, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	n, err := io.Copy(w, resp.Body)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		// The player went away, e.g. to seek.
		p.log.DebugContext(ctx, "jellyfin: stream: client disconnected", "bytes", n)
	default:
		p.log.WarnContext(ctx, "jellyfin: stream: copy", "bytes", n, "error", err)
		logging.Annotate(ctx, "error", "stream copy: "+err.Error())
	}
}

// playing reports the start of a playback.
func (p *plugin) playing(w http.ResponseWriter, r *http.Request) {
	p.playbackReport(w, r, reportStart)
}

// progress reports the position of a playback.
func (p *plugin) progress(w http.ResponseWriter, r *http.Request) {
	p.playbackReport(w, r, reportProgress)
}

// stopped reports the end of a playback.
func (p *plugin) stopped(w http.ResponseWriter, r *http.Request) {
	p.playbackReport(w, r, reportStop)
}

// Kinds of playback reports.
const (
	reportStart    = "start"
	reportProgress = "progress"
	reportStop     = "stop"
)

// playbackReport handles the start, progress and stop reports. They map
// to a playback session through the PlaySessionId, remembered in the kv
// store; a start or progress report for a play session without a live
// playback session starts one. Reports for items that cannot be played
// are ignored.
func (p *plugin) playbackReport(w http.ResponseWriter, r *http.Request, kind string) {
	c, ok := p.authenticate(w, r)
	if !ok {
		return
	}
	var report PlaybackReport
	if !p.decodeBody(w, r, &report) {
		return
	}
	ctx := r.Context()
	p.log.DebugContext(ctx, "jellyfin: playback report", "kind", kind, "item_id", report.ItemId,
		"play_session_id", report.PlaySessionId, "position_ms", positionMs(report),
		"paused", report.IsPaused, "play_method", report.PlayMethod)
	if p.playback == nil {
		p.log.DebugContext(ctx, "jellyfin: playback report ignored", "reason", "no playback module")
	} else {
		var err error
		if kind == reportStop {
			err = p.stopPlayback(ctx, c, report)
		} else {
			err = p.reportPlayback(ctx, c, report, kind)
		}
		if err != nil {
			p.internalError(w, r, "playback report", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// pingPlayback keeps the playback session of a play session alive.
func (p *plugin) pingPlayback(w http.ResponseWriter, r *http.Request) {
	c, ok := p.authenticate(w, r)
	if !ok {
		return
	}
	if p.playback != nil {
		ctx := r.Context()
		playSessionID := lowerQuery(r)["playsessionid"]
		key := playSessionKey(c, playSessionID)
		id, found, err := p.mappedSession(ctx, key)
		switch {
		case err != nil:
		case !found:
			p.log.WarnContext(ctx, "jellyfin: report for unknown play session", "kind", "ping",
				"play_session_id", playSessionID)
		default:
			p.log.DebugContext(ctx, "jellyfin: playback ping", "play_session_id", playSessionID,
				"playback_session_id", id)
			if err = p.playback.Heartbeat(ctx, id); err == nil {
				err = p.renewMapping(ctx, key, id)
			} else if errors.Is(err, playback.ErrNotFound) {
				p.log.DebugContext(ctx, "jellyfin: playback session expired", "play_session_id", playSessionID,
					"playback_session_id", id)
			}
		}
		if err != nil && !errors.Is(err, playback.ErrNotFound) {
			p.internalError(w, r, "playback ping", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// playSessionKey is the kv key of the playback session of a play session
// of a profile. Clients that send no PlaySessionId get one per identity
// session.
func playSessionKey(c caller, playSessionID string) string {
	if playSessionID == "" {
		playSessionID = "session-" + c.session.ID.String()
	}
	return "playsessions/" + c.profile.ID.String() + "/" + strings.ToLower(playSessionID)
}

// mappedSession returns the playback session of a play session, if any.
func (p *plugin) mappedSession(ctx context.Context, key string) (uuid.UUID, bool, error) {
	value, err := p.kv.Get(ctx, key)
	if errors.Is(err, kv.ErrNotFound) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	id, err := uuid.ParseBytes(value)
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, nil
}

// renewMapping extends the life of a play session mapping. A mapping
// that expired meanwhile is created again.
func (p *plugin) renewMapping(ctx context.Context, key string, id uuid.UUID) error {
	err := p.kv.Update(ctx, key, []byte(id.String()), playSessionTTL)
	if errors.Is(err, kv.ErrNotFound) {
		err = p.kv.Create(ctx, key, []byte(id.String()), playSessionTTL)
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
func (p *plugin) reportPlayback(ctx context.Context, c caller, report PlaybackReport, kind string) error {
	key := playSessionKey(c, report.PlaySessionId)
	id, found, err := p.mappedSession(ctx, key)
	if err != nil {
		return err
	}
	if found {
		p.log.DebugContext(ctx, "jellyfin: play session found", "play_session_id", report.PlaySessionId,
			"playback_session_id", id)
		_, err := p.playback.Progress(ctx, id, playbackrouter.ProgressRequestV1{
			PositionMs: positionMs(report),
			Paused:     report.IsPaused,
		})
		if !errors.Is(err, playback.ErrNotFound) {
			if err != nil {
				return err
			}
			return p.renewMapping(ctx, key, id)
		}
		// The playback session timed out: start another one.
		p.log.DebugContext(ctx, "jellyfin: playback session expired", "play_session_id", report.PlaySessionId,
			"playback_session_id", id)
	} else if kind != reportStart {
		p.log.WarnContext(ctx, "jellyfin: report for unknown play session", "kind", kind,
			"play_session_id", report.PlaySessionId, "item_id", report.ItemId)
	}

	sess, ok, err := p.startPlayback(ctx, c, report)
	if err != nil || !ok {
		return err
	}
	if found {
		p.log.DebugContext(ctx, "jellyfin: play session mapped", "play_session_id", report.PlaySessionId,
			"playback_session_id", sess.ID, "replaces", id)
		return p.renewMapping(ctx, key, sess.ID)
	}
	err = p.kv.Create(ctx, key, []byte(sess.ID.String()), playSessionTTL)
	if errors.Is(err, kv.ErrExists) {
		// A concurrent report of the same play session started its own
		// playback session first: keep that one.
		p.log.DebugContext(ctx, "jellyfin: play session mapped concurrently", "play_session_id", report.PlaySessionId,
			"dropped_playback_session_id", sess.ID)
		if err := p.playback.Stop(ctx, sess.ID, playbackrouter.StopRequestV1{}); err != nil &&
			!errors.Is(err, playback.ErrNotFound) {
			return err
		}
		return p.reportPlayback(ctx, c, report, kind)
	}
	if err == nil {
		p.log.DebugContext(ctx, "jellyfin: play session mapped", "play_session_id", report.PlaySessionId,
			"playback_session_id", sess.ID)
	}
	return err
}

// startPlayback starts a playback session for the item of a report. It
// returns false if the item is not a movie or an episode.
func (p *plugin) startPlayback(ctx context.Context, c caller, report PlaybackReport) (playbackrouter.PlaybackSessionV1, bool, error) {
	ignored := func(reason string) (playbackrouter.PlaybackSessionV1, bool, error) {
		p.log.DebugContext(ctx, "jellyfin: playback report ignored", "reason", reason,
			"item_id", report.ItemId, "play_session_id", report.PlaySessionId)
		return playbackrouter.PlaybackSessionV1{}, false, nil
	}
	itemID, err := uuid.Parse(report.ItemId)
	if err != nil {
		return ignored("invalid ItemId")
	}
	if p.library == nil {
		return ignored("no library module")
	}
	ref, err := p.findItem(ctx, itemID)
	if err != nil {
		return playbackrouter.PlaybackSessionV1{}, false, err
	}
	var fileID uuid.UUID
	switch {
	case ref.movie != nil:
		fileID = ref.movie.FileID
	case ref.episode != nil:
		fileID = ref.episode.FileID
	default:
		return ignored("item is not a movie or an episode")
	}
	sess, err := p.playback.CreateSession(ctx, playbackrouter.CreatePlaybackSessionRequestV1{
		AccountID: c.session.AccountID,
		ProfileID: c.session.ProfileID,
		DeviceID:  c.session.DeviceID,
		ItemID:    itemID,
		FileID:    fileID,
		// Every source is served as is: direct play and direct stream
		// (the file through /Videos/{id}/stream) are both direct play.
		PlayMethod: "direct_play",
		PositionMs: positionMs(report),
		Paused:     report.IsPaused,
	})
	return sess, err == nil, err
}

// stopPlayback stops the playback session of a play session, if any.
func (p *plugin) stopPlayback(ctx context.Context, c caller, report PlaybackReport) error {
	key := playSessionKey(c, report.PlaySessionId)
	id, found, err := p.mappedSession(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		p.log.WarnContext(ctx, "jellyfin: report for unknown play session", "kind", reportStop,
			"play_session_id", report.PlaySessionId, "item_id", report.ItemId)
		return nil
	}
	p.log.DebugContext(ctx, "jellyfin: play session stopped", "play_session_id", report.PlaySessionId,
		"playback_session_id", id)
	req := playbackrouter.StopRequestV1{}
	if report.PositionTicks != nil {
		pos := positionMs(report)
		req.PositionMs = &pos
	}
	if err := p.playback.Stop(ctx, id, req); err != nil && !errors.Is(err, playback.ErrNotFound) {
		return err
	}
	if err := p.kv.Delete(ctx, key); err != nil && !errors.Is(err, kv.ErrNotFound) {
		return err
	}
	return nil
}

// utcTime answers the time of the server, for clients to measure their
// clock offset (SyncPlay).
func (p *plugin) utcTime(w http.ResponseWriter, _ *http.Request) {
	received := time.Now().UTC()
	writeJSON(w, http.StatusOK, UtcTimeResponse{
		RequestReceptionTime:     received,
		ResponseTransmissionTime: time.Now().UTC(),
	})
}

// Bounds of the size of GET /Playback/BitrateTest.
const (
	bitrateTestDefaultSize = 102400
	bitrateTestMaxSize     = 100_000_000
)

// bitrateTest answers size bytes, which the client downloads to measure
// the bandwidth to the server (jellyfin-web does it before playing).
func (p *plugin) bitrateTest(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	size := int64(bitrateTestDefaultSize)
	if s := lowerQuery(r)["size"]; s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 || n > bitrateTestMaxSize {
			badRequest(w, r, "invalid size")
			return
		}
		size = n
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(w, zeroReader{}, size)
}

// zeroReader reads an endless stream of zero bytes.
type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}
