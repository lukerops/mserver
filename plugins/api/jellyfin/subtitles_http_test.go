package jellyfin_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

const heatSubtitle = "1\n00:00:00,000 --> 00:00:01,000\nHello\n"

func TestExternalSubtitleStreams(t *testing.T) {
	c, auth, _ := startWithMovies(t)
	_, token, _ := strings.Cut(auth, `Token="`)
	token = strings.TrimSuffix(token, `"`)

	page, err := c.modules.Library.ListMovies(t.Context(), &libraryv1.ListMoviesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	alien, heat := page.GetMovies()[0], page.GetMovies()[1]
	heatID, alienID := hex(heat.GetId()), hex(alien.GetId())
	base := "/Videos/" + heatID + "/" + heatID + "/Subtitles/2/"

	// The subtitle as it is, as the DeliveryUrl says (with Range, which
	// the storage honors).
	res, body := c.raw("GET", base+"Stream.srt?api_key="+token, nil)
	if res.StatusCode != http.StatusOK || string(body) != heatSubtitle ||
		res.Header.Get("Content-Type") != "application/x-subrip" ||
		res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("GET srt = %d, %q, %v", res.StatusCode, body, res.Header)
	}
	res, body = c.raw("GET", base+"Stream.srt?api_key="+token, http.Header{"Range": {"bytes=0-1"}})
	if res.StatusCode != http.StatusPartialContent || string(body) != "1\n" {
		t.Errorf("GET srt with range = %d, %q", res.StatusCode, body)
	}
	res, body = c.raw("HEAD", base+"Stream.srt?api_key="+token, nil)
	if res.StatusCode != http.StatusOK || len(body) != 0 {
		t.Errorf("HEAD srt = %d, %d bytes", res.StatusCode, len(body))
	}

	// With the start position, which is ignored.
	res, body = c.raw("GET", base+"0/Stream.srt?api_key="+token, nil)
	if res.StatusCode != http.StatusOK || string(body) != heatSubtitle {
		t.Errorf("GET srt with start position = %d, %q", res.StatusCode, body)
	}

	// As WebVTT, converted.
	res, body = c.raw("GET", base+"Stream.vtt?api_key="+token, nil)
	if res.StatusCode != http.StatusOK || string(body) != "WEBVTT\n\n1\n00:00:00.000 --> 00:00:01.000\nHello\n" ||
		!strings.HasPrefix(res.Header.Get("Content-Type"), "text/vtt") {
		t.Errorf("GET vtt = %d, %q, %v", res.StatusCode, body, res.Header)
	}

	for name, tc := range map[string]struct {
		path   string
		status int
	}{
		"no token":              {base + "Stream.srt", http.StatusUnauthorized},
		"embedded stream":       {"/Videos/" + heatID + "/" + heatID + "/Subtitles/1/Stream.srt?api_key=" + token, http.StatusNotFound},
		"unknown index":         {"/Videos/" + heatID + "/" + heatID + "/Subtitles/3/Stream.srt?api_key=" + token, http.StatusNotFound},
		"invalid index":         {"/Videos/" + heatID + "/" + heatID + "/Subtitles/x/Stream.srt?api_key=" + token, http.StatusNotFound},
		"unknown format":        {base + "Stream.ass?api_key=" + token, http.StatusNotFound},
		"not a stream":          {base + "other.srt?api_key=" + token, http.StatusNotFound},
		"invalid start":         {base + "x/Stream.srt?api_key=" + token, http.StatusNotFound},
		"js":                    {base + "0/Stream.js?api_key=" + token, http.StatusOK},
		"json":                  {base + "0/Stream.json?api_key=" + token, http.StatusOK},
		"other media source":    {"/Videos/" + heatID + "/" + alienID + "/Subtitles/2/Stream.srt?api_key=" + token, http.StatusNotFound},
		"item without subtitle": {"/Videos/" + alienID + "/" + alienID + "/Subtitles/2/Stream.srt?api_key=" + token, http.StatusNotFound},
		"unknown item":          {"/Videos/" + hex(heat.GetLibraryId()) + "/" + heatID + "/Subtitles/2/Stream.srt?api_key=" + token, http.StatusNotFound},
	} {
		if res, _ := c.raw("GET", tc.path, nil); res.StatusCode != tc.status {
			t.Errorf("%s: GET %s = %d, want %d", name, tc.path, res.StatusCode, tc.status)
		}
	}

	// A client fetches the DeliveryUrl as it is, with no authorization
	// header: the URL carries the token. The device profile decides the
	// format: the original when the device plays it, vtt when it needs the
	// only conversion. jellyfin-web gets the cues in JSON by replacing .vtt
	// with .js.
	device := func(subtitleFormat string) map[string]any {
		return map[string]any{"DeviceProfile": map[string]any{
			"DirectPlayProfiles": []map[string]string{{"Container": "mkv", "Type": "Video"}},
			"SubtitleProfiles":   []map[string]string{{"Format": subtitleFormat, "Method": "External"}},
		}}
	}
	var info struct {
		MediaSources []struct{ MediaStreams []mediaStream }
	}

	// Without a profile, the original format is kept.
	c.expect("GET", "/Items/"+heatID+"/PlaybackInfo", auth, nil, &info, http.StatusOK)
	if len(info.MediaSources) != 1 || len(info.MediaSources[0].MediaStreams) != 3 {
		t.Fatalf("playback info = %+v", info)
	}
	if got := info.MediaSources[0].MediaStreams[2].DeliveryUrl; !strings.HasSuffix(got, "/0/Stream.srt?ApiKey="+token) {
		t.Errorf("DeliveryUrl without a profile = %q, want .../0/Stream.srt with the ApiKey", got)
	}

	// A device that plays srt as it is: no conversion.
	c.expect("POST", "/Items/"+heatID+"/PlaybackInfo", auth, device("srt"), &info, http.StatusOK)
	if got := info.MediaSources[0].MediaStreams[2].DeliveryUrl; !strings.HasSuffix(got, "/0/Stream.srt?ApiKey="+token) {
		t.Errorf("DeliveryUrl with srt = %q, want .../0/Stream.srt", got)
	}

	// jellyfin-web plays only vtt: the srt is converted.
	c.expect("POST", "/Items/"+heatID+"/PlaybackInfo", auth, device("vtt"), &info, http.StatusOK)
	deliveryURL := info.MediaSources[0].MediaStreams[2].DeliveryUrl
	if !strings.HasSuffix(deliveryURL, "/0/Stream.vtt?ApiKey="+token) {
		t.Fatalf("DeliveryUrl with vtt = %q, want .../0/Stream.vtt with the ApiKey", deliveryURL)
	}
	if res, body := c.raw("GET", deliveryURL, nil); res.StatusCode != http.StatusOK ||
		!strings.HasPrefix(string(body), "WEBVTT\n") {
		t.Errorf("GET vtt %s = %d, %q", deliveryURL, res.StatusCode, body)
	}

	jsURL := strings.Replace(deliveryURL, ".vtt", ".js", 1)
	res, body = c.raw("GET", jsURL, nil)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("GET js %s = %d, %v", jsURL, res.StatusCode, res.Header)
	}
	var trackInfo struct {
		TrackEvents []struct {
			ID, Text           string
			StartPositionTicks int64
			EndPositionTicks   int64
		}
	}
	if err := json.Unmarshal(body, &trackInfo); err != nil {
		t.Fatalf("GET js %s: %v, %q", jsURL, err, body)
	}
	if len(trackInfo.TrackEvents) != 1 || trackInfo.TrackEvents[0].ID != "1" ||
		trackInfo.TrackEvents[0].Text != "Hello" || trackInfo.TrackEvents[0].StartPositionTicks != 0 ||
		trackInfo.TrackEvents[0].EndPositionTicks != 10_000_000 {
		t.Errorf("track events = %+v", trackInfo.TrackEvents)
	}
}
