package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// Bounds of the size of GET /Playback/BitrateTest.
const (
	bitrateTestDefaultSize = 102400
	bitrateTestMaxSize     = 100_000_000
)

// playbackInfo answers the media sources of a movie or episode, all of them
// played as they are (no transcoding). A source the device profile of the
// client cannot play directly is an error "NoCompatibleStream". The
// PlaySessionId is new on every call: the session starts only when the client
// reports it is playing.
func (h *Handler) playbackInfo(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var req service.PlaybackInfoRequest
	if r.Method == http.MethodPost && !h.decodeBody(w, r, &req) {
		return
	}
	var queryAudioIndex *int
	if n, err := strconv.Atoi(lowerQuery(r)["audiostreamindex"]); err == nil {
		queryAudioIndex = &n
	}
	v, err := h.svc.Video(r.Context(), id)
	if err != nil {
		h.serviceError(w, r, "playback info", err)
		return
	}
	res := h.svc.PlaybackInfo(r.Context(), v, req, queryAudioIndex)
	signSources(res.MediaSources, requestAuthorization(r).Token)
	writeJSON(w, http.StatusOK, res)
}

// stream serves the file of a movie or episode as is, at
// /Videos/{itemId}/stream or /Videos/{itemId}/stream.{container}, with Range
// requests (seeking). The content comes from the storage, whose URL for the
// file the storage module resolves.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	if file := r.PathValue("file"); file != "stream" && !strings.HasPrefix(file, "stream.") {
		notFound(w, r, "unsupported stream file name")
		return
	}
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Video(r.Context(), id)
	if err != nil {
		h.serviceError(w, r, "stream", err)
		return
	}
	resp := h.fetchFile(w, r, v.Item.Id, v.FileID, true)
	if resp == nil {
		return
	}
	defer resp.Body.Close()
	h.copyFile(w, r, resp)
}

// subtitle serves the subtitle file at
// /Videos/{itemId}/{mediaSourceId}/Subtitles/{index}/Stream.{format} (and the
// same with the start position in ticks before Stream, which is ignored:
// nothing is cut from the start of the video). The index is the one of the
// stream in the media source. The content comes from the storage, in the
// format of the file when the client asks for it (any format, see
// service.SubtitleConvert). Only srt -> vtt is converted, to WebVTT or to the
// JSON of the cues (SubtitleTrackInfo), which jellyfin-web fetches from the
// DeliveryUrl by replacing the extension (.vtt -> .js).
func (h *Handler) subtitle(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Video(r.Context(), id)
	if err != nil {
		h.serviceError(w, r, "subtitle", err)
		return
	}
	if source := r.PathValue("mediaSourceId"); !strings.EqualFold(source, v.Item.Id) {
		notFound(w, r, "unknown media source")
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < v.SubtitleBase || index-v.SubtitleBase >= len(v.Subtitles) {
		notFound(w, r, "unknown subtitle stream")
		return
	}
	sub := v.Subtitles[index-v.SubtitleBase]
	conversion, ok := service.SubtitleConvert(sub.Format, format)
	if !ok {
		notFound(w, r, "unsupported subtitle format")
		return
	}

	// A converted subtitle needs the whole file.
	passthrough := conversion == service.SubtitlePassthrough
	resp := h.fetchFile(w, r, v.Item.Id, sub.FileID, passthrough)
	if resp == nil {
		return
	}
	defer resp.Body.Close()
	if passthrough {
		h.copyFile(w, r, resp)
		return
	}

	const maxSubtitleSize = 4 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSubtitleSize+1))
	switch {
	case err != nil:
		h.log.WarnContext(r.Context(), "jellyfin: subtitle: read",
			slog.String("file_id", sub.FileID), slog.String("error", err.Error()))
		unavailable(w, r, "storage unavailable")
		return
	case len(data) > maxSubtitleSize:
		notFound(w, r, "subtitle file too large")
		return
	}

	var body []byte
	contentType := "text/vtt; charset=utf-8"
	if conversion == service.SubtitleJSON {
		body, err = json.Marshal(service.SRTToTrackInfo(data))
		if err != nil {
			h.internalError(w, r, "encode subtitle", err)
			return
		}
		contentType = "application/json; charset=utf-8"
	} else {
		body = service.SRTToVTT(data)
	}
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// playing reports the start of a playback.
func (h *Handler) playing(w http.ResponseWriter, r *http.Request) {
	h.playbackReport(w, r, h.svc.StartReport)
}

// progress reports the position of a playback.
func (h *Handler) progress(w http.ResponseWriter, r *http.Request) {
	h.playbackReport(w, r, h.svc.ProgressReport)
}

// stopped reports the end of a playback.
func (h *Handler) stopped(w http.ResponseWriter, r *http.Request) {
	h.playbackReport(w, r, h.svc.StopReport)
}

// playbackReport handles the start, progress and stop reports. They map to a
// playback session through the PlaySessionId, remembered in the kv store; a
// start or progress report for a play session without a live playback session
// starts one. Reports for items that cannot be played are ignored.
func (h *Handler) playbackReport(w http.ResponseWriter, r *http.Request, report func(context.Context, service.Caller, service.PlaybackReport) error) {
	c, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var body service.PlaybackReport
	if !h.decodeBody(w, r, &body) {
		return
	}
	if err := report(r.Context(), c, body); err != nil {
		h.internalError(w, r, "playback report", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pingPlayback keeps the playback session of a play session alive.
func (h *Handler) pingPlayback(w http.ResponseWriter, r *http.Request) {
	c, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if err := h.svc.Ping(r.Context(), c, lowerQuery(r)["playsessionid"]); err != nil {
		h.internalError(w, r, "playback ping", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// utcTime answers the time of the server, for clients to measure their clock
// offset (SyncPlay).
func (h *Handler) utcTime(w http.ResponseWriter, _ *http.Request) {
	received := time.Now().UTC()
	writeJSON(w, http.StatusOK, service.UtcTimeResponse{
		RequestReceptionTime:     received,
		ResponseTransmissionTime: time.Now().UTC(),
	})
}

// bitrateTest answers size bytes, which the client downloads to measure the
// bandwidth to the server (jellyfin-web does it before playing).
func (h *Handler) bitrateTest(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
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
	header := w.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Length", strconv.FormatInt(size, 10))
	header.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(w, zeroReader{}, size)
}

// streamRequestHeaders are the headers of the player forwarded to the storage,
// for Range requests (seeking) and caching.
var streamRequestHeaders = []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match"}

// streamResponseHeaders are the headers of the storage forwarded to the player.
var streamResponseHeaders = []string{
	"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag",
}

// fetchFile requests a file from the storage, with the method of r and, when
// forward is set, the headers of the player for Range requests (seeking) and
// caching. The caller closes the body of the response, and copies it with
// copyFile. When the file cannot be had, it answers r and returns nil.
func (h *Handler) fetchFile(w http.ResponseWriter, r *http.Request, itemID, fileID string, forward bool) *http.Response {
	ctx := r.Context()
	h.log.DebugContext(ctx, "jellyfin: stream: storage request",
		slog.String("item_id", itemID),
		slog.String("file_id", fileID),
		slog.String("range", r.Header.Get("Range")),
	)
	url, err := h.svc.ResolveFile(ctx, fileID)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return nil
	case errors.Is(err, service.ErrNotFound):
		h.log.WarnContext(ctx, "jellyfin: stream: file not found in storage",
			slog.String("file_id", fileID), slog.String("error", err.Error()))
		notFound(w, r, "file not found in storage")
		return nil
	case errors.Is(err, service.ErrUnavailable):
		h.log.WarnContext(ctx, "jellyfin: stream: storage unavailable",
			slog.String("file_id", fileID), slog.String("error", err.Error()))
		unavailable(w, r, "storage unavailable")
		return nil
	default:
		h.internalError(w, r, "resolve file", err)
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, url, nil)
	if err != nil {
		h.internalError(w, r, "open file", err)
		return nil
	}
	if forward {
		for _, name := range streamRequestHeaders {
			if v := r.Header.Get(name); v != "" {
				req.Header.Set(name, v)
			}
		}
	}
	resp, err := h.files.Do(req)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		h.log.DebugContext(ctx, "jellyfin: stream: client went away", slog.String("error", err.Error()))
		annotate(ctx, slog.String("error", "client went away"))
		return nil
	default:
		h.log.WarnContext(ctx, "jellyfin: stream: storage unavailable",
			slog.String("file_id", fileID), slog.String("error", err.Error()))
		unavailable(w, r, "storage unavailable")
		return nil
	}
	h.log.DebugContext(ctx, "jellyfin: stream: storage response",
		slog.Int("status", resp.StatusCode),
		slog.Int64("content_length", resp.ContentLength),
		slog.String("content_range", resp.Header.Get("Content-Range")),
	)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		notFound(w, r, "file not found in storage")
		return nil
	case resp.StatusCode >= http.StatusInternalServerError:
		resp.Body.Close()
		unavailable(w, r, "storage unavailable")
		return nil
	}
	return resp
}

// copyFile answers r with the response of the storage as it is: status, the
// headers of streamResponseHeaders and the body.
func (h *Handler) copyFile(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	ctx := r.Context()
	header := w.Header()
	for _, name := range streamResponseHeaders {
		if v := resp.Header.Get(name); v != "" {
			header.Set(name, v)
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
		h.log.DebugContext(ctx, "jellyfin: stream: client disconnected", slog.Int64("bytes", n))
	default:
		h.log.WarnContext(ctx, "jellyfin: stream: copy",
			slog.Int64("bytes", n), slog.String("error", err.Error()))
		annotate(ctx, slog.String("error", "stream copy: "+err.Error()))
	}
}

// zeroReader reads an endless stream of zero bytes.
type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}
