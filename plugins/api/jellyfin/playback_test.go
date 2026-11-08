package jellyfin_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	libraryrouter "github.com/lukerops/mserver/internal/modules/library/router"
	playbackrouter "github.com/lukerops/mserver/internal/modules/playback/router"
)

type playbackInfo struct {
	MediaSources []struct {
		Id, Protocol, Container                                       string
		SupportsDirectPlay, SupportsDirectStream, SupportsTranscoding bool
	}
	PlaySessionId string
	ErrorCode     string
}

// raw sends a request without checking it.
func (c client) raw(method, path string, h http.Header) (*http.Response, []byte) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.t.Context(), method, c.base+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range h {
		req.Header[k] = v
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return res, body
}

func sessions(t *testing.T, c client) []playbackrouter.PlaybackSessionV1 {
	t.Helper()
	list, err := c.modules.Playback.ListSessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPlayback(t *testing.T) {
	c, auth, _ := startWithMovies(t)
	_, token, _ := strings.Cut(auth, `Token="`)
	token = strings.TrimSuffix(token, `"`)

	page, err := c.modules.Library.ListMovies(t.Context(), uuid.Nil, libraryrouter.MovieQueryV1{})
	if err != nil {
		t.Fatal(err)
	}
	heat := page.Movies[1]
	if heat.Title != "Heat" {
		t.Fatalf("movies = %+v", page.Movies)
	}
	heatID := hex(heat.ID)

	// The device profile of a browser that plays Matroska.
	var info playbackInfo
	c.expect("POST", "/Items/"+heatID+"/PlaybackInfo?UserId=x&AudioStreamIndex=1", auth, map[string]any{
		"DeviceProfile": map[string]any{"DirectPlayProfiles": []map[string]string{
			{"Container": "webm", "Type": "Video", "VideoCodec": "vp8,vp9,av1", "AudioCodec": "vorbis,opus"},
			{"Container": "mp4,m4v,mkv", "Type": "Video", "VideoCodec": "h264,hevc", "AudioCodec": "aac,mp3,opus"},
		}},
	}, &info, http.StatusOK)
	if len(info.MediaSources) != 1 || info.PlaySessionId == "" || info.ErrorCode != "" {
		t.Fatalf("playback info = %+v", info)
	}
	if s := info.MediaSources[0]; s.Id != heatID || s.Container != "mkv" || s.Protocol != "File" ||
		!s.SupportsDirectPlay || !s.SupportsDirectStream || s.SupportsTranscoding {
		t.Errorf("media source = %+v", s)
	}
	// A device that plays no Matroska.
	var none playbackInfo
	c.expect("POST", "/Items/"+heatID+"/PlaybackInfo", auth, map[string]any{
		"DeviceProfile": map[string]any{"DirectPlayProfiles": []map[string]string{
			{"Container": "mp4", "Type": "Video"},
			{"Container": "mkv", "Type": "Audio"},
		}},
	}, &none, http.StatusOK)
	if none.ErrorCode != "NoCompatibleStream" || len(none.MediaSources) != 0 {
		t.Errorf("incompatible playback info = %+v", none)
	}
	// Without a profile, everything plays.
	c.expect("GET", "/Items/"+heatID+"/PlaybackInfo", auth, nil, &none, http.StatusOK)
	if len(none.MediaSources) != 1 {
		t.Errorf("playback info without profile = %+v", none)
	}
	// jellyfin-web sends numbers as strings.
	var web playbackInfo
	c.expect("POST", "/Items/"+heatID+"/PlaybackInfo", auth, map[string]any{
		"UserId": "x", "StartTimeTicks": 0, "IsPlayback": true, "AutoOpenLiveStream": true,
		"AudioStreamIndex": "1", "SubtitleStreamIndex": "-1", "MediaSourceId": heatID,
		"DeviceProfile": map[string]any{
			"MaxStreamingBitrate": 120000000,
			"DirectPlayProfiles": []map[string]string{
				{"Container": "mkv", "Type": "Video", "VideoCodec": "h264,hevc", "AudioCodec": "aac,mp3,opus"},
			},
			"TranscodingProfiles": []map[string]any{
				{"Container": "ts", "Type": "Video", "MaxAudioChannels": "2", "MinSegments": "1", "BreakOnNonKeyFrames": true},
			},
		},
	}, &web, http.StatusOK)
	if len(web.MediaSources) != 1 || web.ErrorCode != "" {
		t.Errorf("jellyfin-web playback info = %+v", web)
	}
	c.expect("GET", "/Items/"+hex(heat.LibraryID)+"/PlaybackInfo", auth, nil, nil, http.StatusNotFound)
	c.expect("GET", "/Items/"+heatID+"/PlaybackInfo", "", nil, nil, http.StatusUnauthorized)

	// The stream, as the video element asks for it.
	stream := "/Videos/" + heatID + "/stream.mkv?Static=true&mediaSourceId=" + heatID + "&api_key=" + token
	res, body := c.raw("GET", stream, http.Header{"Range": {"bytes=0-99"}})
	if res.StatusCode != http.StatusPartialContent || len(body) != 100 ||
		res.Header.Get("Content-Type") != "video/x-matroska" || res.Header.Get("Accept-Ranges") != "bytes" ||
		!strings.HasPrefix(res.Header.Get("Content-Range"), "bytes 0-99/") ||
		res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("GET stream = %d, %d bytes, %v", res.StatusCode, len(body), res.Header)
	}
	size := strings.TrimPrefix(res.Header.Get("Content-Range"), "bytes 0-99/")
	res, body = c.raw("HEAD", "/Videos/"+heatID+"/stream?api_key="+token, nil)
	if res.StatusCode != http.StatusOK || len(body) != 0 || res.Header.Get("Content-Length") != size {
		t.Errorf("HEAD stream = %d, %v", res.StatusCode, res.Header)
	}
	res, body = c.raw("GET", "/Videos/"+heatID+"/stream?api_key="+token, nil)
	if n, _ := strconv.Atoi(size); res.StatusCode != http.StatusOK || len(body) != n {
		t.Errorf("GET whole stream = %d, %d bytes, want %s", res.StatusCode, len(body), size)
	}
	if res, _ := c.raw("GET", "/Videos/"+heatID+"/stream.mkv", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("stream without token = %d", res.StatusCode)
	}
	if res, _ := c.raw("GET", "/Videos/"+heatID+"/master.m3u8?api_key="+token, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("HLS = %d", res.StatusCode)
	}

	// Play, pause, resume and close the player.
	playSession := info.PlaySessionId
	report := func(path string, ticks int64, paused bool) {
		t.Helper()
		c.expect("POST", path, auth, map[string]any{
			"ItemId": heatID, "MediaSourceId": heatID, "PlaySessionId": playSession,
			"PositionTicks": ticks, "IsPaused": paused, "PlayMethod": "DirectStream",
		}, nil, http.StatusNoContent)
	}
	report("/Sessions/Playing", 0, false)
	list := sessions(t, c)
	if len(list) != 1 || list[0].ItemID != heat.ID || list[0].FileID != heat.FileID || list[0].PlayMethod != "direct_play" {
		t.Fatalf("sessions after start = %+v", list)
	}
	id := list[0].ID
	report("/Sessions/Playing/Progress", 5000*10_000, true)
	if list := sessions(t, c); len(list) != 1 || list[0].ID != id || !list[0].Paused || list[0].PositionMs != 5000 {
		t.Fatalf("sessions after pause = %+v", list)
	}
	report("/Sessions/Playing/Progress", 5000*10_000, false)
	c.expect("POST", "/Sessions/Playing/Ping?playSessionId="+playSession, auth, nil, nil, http.StatusNoContent)
	c.expect("POST", "/Sessions/Playing/Ping?playSessionId=unknown", auth, nil, nil, http.StatusNoContent)
	if list := sessions(t, c); len(list) != 1 || list[0].Paused || list[0].Sequence != 5 {
		t.Fatalf("sessions after resume = %+v", list)
	}
	report("/Sessions/Playing/Stopped", 7000*10_000, false)
	if list := sessions(t, c); len(list) != 0 {
		t.Fatalf("sessions after stop = %+v", list)
	}
	// A second stop is harmless.
	report("/Sessions/Playing/Stopped", 7000*10_000, false)

	// A progress report of a play session without a live playback session
	// (e.g. it timed out) starts one.
	playSession = "other"
	report("/Sessions/Playing/Progress", 1000*10_000, false)
	if list := sessions(t, c); len(list) != 1 || list[0].ID == id || list[0].PositionMs != 1000 {
		t.Fatalf("sessions after orphan progress = %+v", list)
	}
	report("/Sessions/Playing/Stopped", 2000*10_000, false)

	var utc struct{ RequestReceptionTime, ResponseTransmissionTime string }
	c.expect("GET", "/GetUtcTime", "", nil, &utc, http.StatusOK)
	if utc.RequestReceptionTime == "" || utc.ResponseTransmissionTime == "" {
		t.Errorf("utc time = %+v", utc)
	}

	res, body = c.raw("GET", "/Playback/BitrateTest?Size=500000&api_key="+token, nil)
	if res.StatusCode != http.StatusOK || len(body) != 500000 ||
		res.Header.Get("Content-Type") != "application/octet-stream" ||
		res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("bitrate test: status %d, %d bytes, headers %v", res.StatusCode, len(body), res.Header)
	}
	res, body = c.raw("GET", "/Playback/BitrateTest?api_key="+token, nil)
	if res.StatusCode != http.StatusOK || len(body) != 102400 {
		t.Errorf("default bitrate test: status %d, %d bytes", res.StatusCode, len(body))
	}
	c.expect("GET", "/Playback/BitrateTest?size=0", auth, nil, nil, http.StatusBadRequest)
	c.expect("GET", "/Playback/BitrateTest?size=100000001", auth, nil, nil, http.StatusBadRequest)
	c.expect("GET", "/Playback/BitrateTest", "", nil, nil, http.StatusUnauthorized)

	// The preflight allows any header the client asks for.
	res, _ = c.raw("OPTIONS", "/Playback/BitrateTest?Size=500000", http.Header{
		"Origin":                         {"http://localhost:8080"},
		"Access-Control-Request-Method":  {"GET"},
		"Access-Control-Request-Headers": {"authorization,cache-control,x-custom"},
	})
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != "*" ||
		res.Header.Get("Access-Control-Allow-Headers") != "authorization,cache-control,x-custom" {
		t.Errorf("bitrate test preflight: status %d, headers %v", res.StatusCode, res.Header)
	}
}
