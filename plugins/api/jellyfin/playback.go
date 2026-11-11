package jellyfin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
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

// video is a playable item: a movie or an episode.
type video struct {
	item   BaseItemDto
	fileID string
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
		return video{item: p.movieItem(m, true), fileID: m.GetFileId()}, true
	case ref.episode != nil:
		e := ref.episode
		return video{item: p.episodeItem(e, true), fileID: e.GetFileId()}, true
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
		attrs := []slog.Attr{
			slog.String("item_id", v.item.Id),
			slog.String("source_id", source.Id),
			slog.String("container", source.Container),
		}
		if info.DeviceProfile == nil {
			p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback info: direct play",
				append(attrs, slog.String("reason", "no device profile"))...)
		} else {
			d := directPlay(info.DeviceProfile.DirectPlayProfiles, source, audioIndex)
			attrs = append(attrs,
				slog.String("video_codec", d.videoCodec),
				slog.String("audio_codec", d.audioCodec),
				slog.Bool("direct_playable", d.profile >= 0),
			)
			if d.profile < 0 {
				p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback info: source rejected",
					append(attrs,
						slog.String("reason", d.rejected),
						slog.Int("profiles", len(info.DeviceProfile.DirectPlayProfiles)),
					)...)
				continue
			}
			p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback info: direct play",
				append(attrs, slog.Int("profile", d.profile))...)
		}
		source.SupportsDirectPlay = true
		source.SupportsDirectStream = true
		source.SupportsTranscoding = false
		source.Protocol = "File"
		res.MediaSources = append(res.MediaSources, source)
	}
	if len(res.MediaSources) == 0 {
		res.ErrorCode = "NoCompatibleStream"
		annotate(ctx, slog.String("playback_error", res.ErrorCode))
	}
	p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session created",
		slog.String("item_id", v.item.Id),
		slog.String("play_session_id", res.PlaySessionId),
		slog.Int("sources", len(res.MediaSources)),
	)
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

// streamRequestHeaders are the headers of the player forwarded to the
// storage, for Range requests (seeking) and caching.
var streamRequestHeaders = []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match"}

// streamResponseHeaders are the headers of the storage forwarded to the
// player.
var streamResponseHeaders = []string{
	"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag",
}

// stream serves the file of a movie or episode as is, at
// /Videos/{itemId}/stream or /Videos/{itemId}/stream.{container}, with
// Range requests (seeking). The content comes from the storage, whose URL
// for the file the storage module resolves.
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
	p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: stream: storage request",
		slog.String("item_id", v.item.Id),
		slog.String("file_id", v.fileID),
		slog.String("range", r.Header.Get("Range")),
	)
	resolved, err := p.storage.ResolveFile(ctx, &storagev1.ResolveFileRequest{Id: v.fileID})
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return
	case isCode(err, connect.CodeNotFound):
		p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: stream: file not found in storage",
			slog.String("file_id", v.fileID), slog.String("error", err.Error()))
		notFound(w, r, "file not found in storage")
		return
	case isCode(err, connect.CodeUnavailable), isCode(err, connect.CodeFailedPrecondition):
		p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: stream: storage unavailable",
			slog.String("file_id", v.fileID), slog.String("error", err.Error()))
		unavailable(w, r, "storage unavailable")
		return
	default:
		p.internalError(w, r, "resolve file", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, resolved.GetUrl(), nil)
	if err != nil {
		p.internalError(w, r, "open file", err)
		return
	}
	for _, name := range streamRequestHeaders {
		if v := r.Header.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	resp, err := p.files.Do(req)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: stream: client went away",
			slog.String("error", err.Error()))
		annotate(ctx, slog.String("error", "client went away"))
		return
	default:
		p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: stream: storage unavailable",
			slog.String("file_id", v.fileID), slog.String("error", err.Error()))
		unavailable(w, r, "storage unavailable")
		return
	}
	defer resp.Body.Close()
	p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: stream: storage response",
		slog.Int("status", resp.StatusCode),
		slog.Int64("content_length", resp.ContentLength),
		slog.String("content_range", resp.Header.Get("Content-Range")),
	)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		notFound(w, r, "file not found in storage")
		return
	case resp.StatusCode >= http.StatusInternalServerError:
		unavailable(w, r, "storage unavailable")
		return
	}

	h := w.Header()
	for _, name := range streamResponseHeaders {
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
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: stream: client disconnected", slog.Int64("bytes", n))
	default:
		p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: stream: copy",
			slog.Int64("bytes", n), slog.String("error", err.Error()))
		annotate(ctx, slog.String("error", "stream copy: "+err.Error()))
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
	p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback report",
		slog.String("kind", kind),
		slog.String("item_id", report.ItemId),
		slog.String("play_session_id", report.PlaySessionId),
		slog.Int64("position_ms", positionMs(report)),
		slog.Bool("paused", report.IsPaused),
		slog.String("play_method", report.PlayMethod),
	)
	if p.playback == nil {
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback report ignored",
			slog.String("reason", "no playback module"))
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
			p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: report for unknown play session",
				slog.String("kind", "ping"), slog.String("play_session_id", playSessionID))
		default:
			p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback ping",
				slog.String("play_session_id", playSessionID), slog.String("playback_session_id", id))
			_, err = p.playback.HeartbeatSession(ctx, &playbackv1.HeartbeatSessionRequest{Id: id})
			if err == nil {
				err = p.renewMapping(ctx, key, id)
			} else if isCode(err, connect.CodeNotFound) {
				p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback session expired",
					slog.String("play_session_id", playSessionID), slog.String("playback_session_id", id))
			}
		}
		if err != nil && !isCode(err, connect.CodeNotFound) {
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
		playSessionID = "session-" + c.session.GetId()
	}
	return "playsessions/" + c.profile.GetId() + "/" + strings.ToLower(playSessionID)
}

// mappedSession returns the ID of the playback session of a play session,
// if any.
func (p *plugin) mappedSession(ctx context.Context, key string) (string, bool, error) {
	value, err := p.kv.Get(ctx, key)
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
func (p *plugin) renewMapping(ctx context.Context, key, id string) error {
	err := p.kv.Update(ctx, key, []byte(id), playSessionTTL)
	if errors.Is(err, kv.ErrNotFound) {
		err = p.kv.Create(ctx, key, []byte(id), playSessionTTL)
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
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session found",
			slog.String("play_session_id", report.PlaySessionId), slog.String("playback_session_id", id))
		_, err := p.playback.ReportProgress(ctx, &playbackv1.ReportProgressRequest{
			Id:         id,
			PositionMs: positionMs(report),
			Paused:     report.IsPaused,
		})
		if !isCode(err, connect.CodeNotFound) {
			if err != nil {
				return err
			}
			return p.renewMapping(ctx, key, id)
		}
		// The playback session timed out: start another one.
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback session expired",
			slog.String("play_session_id", report.PlaySessionId), slog.String("playback_session_id", id))
	} else if kind != reportStart {
		p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: report for unknown play session",
			slog.String("kind", kind),
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("item_id", report.ItemId),
		)
	}

	sess, err := p.startPlayback(ctx, c, report)
	if err != nil || sess == nil {
		return err
	}
	if found {
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session mapped",
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("playback_session_id", sess.GetId()),
			slog.String("replaces", id),
		)
		return p.renewMapping(ctx, key, sess.GetId())
	}
	err = p.kv.Create(ctx, key, []byte(sess.GetId()), playSessionTTL)
	if errors.Is(err, kv.ErrExists) {
		// A concurrent report of the same play session started its own
		// playback session first: keep that one.
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session mapped concurrently",
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("dropped_playback_session_id", sess.GetId()),
		)
		if _, err := p.playback.StopSession(ctx, &playbackv1.StopSessionRequest{Id: sess.GetId()}); err != nil &&
			!isCode(err, connect.CodeNotFound) {
			return err
		}
		return p.reportPlayback(ctx, c, report, kind)
	}
	if err == nil {
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session mapped",
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("playback_session_id", sess.GetId()),
		)
	}
	return err
}

// startPlayback starts a playback session for the item of a report. It
// returns nil if the item is not a movie or an episode.
func (p *plugin) startPlayback(ctx context.Context, c caller, report PlaybackReport) (*playbackv1.Session, error) {
	ignored := func(reason string) (*playbackv1.Session, error) {
		p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: playback report ignored",
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
	if p.library == nil {
		return ignored("no library module")
	}
	ref, err := p.findItem(ctx, itemID)
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
	res, err := p.playback.StartSession(ctx, &playbackv1.StartSessionRequest{
		AccountId: c.session.GetAccountId(),
		ProfileId: c.session.GetProfileId(),
		DeviceId:  c.session.GetDeviceId(),
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
func (p *plugin) stopPlayback(ctx context.Context, c caller, report PlaybackReport) error {
	key := playSessionKey(c, report.PlaySessionId)
	id, found, err := p.mappedSession(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: report for unknown play session",
			slog.String("kind", reportStop),
			slog.String("play_session_id", report.PlaySessionId),
			slog.String("item_id", report.ItemId),
		)
		return nil
	}
	p.log.LogAttrs(ctx, slog.LevelDebug, "jellyfin: play session stopped",
		slog.String("play_session_id", report.PlaySessionId), slog.String("playback_session_id", id))
	req := &playbackv1.StopSessionRequest{Id: id}
	if report.PositionTicks != nil {
		pos := positionMs(report)
		req.PositionMs = &pos
	}
	if _, err := p.playback.StopSession(ctx, req); err != nil && !isCode(err, connect.CodeNotFound) {
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
