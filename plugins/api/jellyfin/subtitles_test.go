package jellyfin

import (
	"testing"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

func TestSubtitleLanguage(t *testing.T) {
	for _, tt := range []struct {
		tag, code, name string
	}{
		{"", "", ""},
		{"en", "eng", "English"},
		{"pt-BR", "por", "Portuguese (Brazil)"},
		{"pt", "por", "Portuguese"},
		{"zh-Hant-TW", "chi", "Chinese"},
		{"eng", "eng", "English"},
		{"fil", "fil", "FIL"},
	} {
		code, name := subtitleLanguage(tt.tag)
		if code != tt.code || name != tt.name {
			t.Errorf("subtitleLanguage(%q) = %q, %q; want %q, %q", tt.tag, code, name, tt.code, tt.name)
		}
	}
}

func TestExternalSubtitleStream(t *testing.T) {
	for _, tt := range []struct {
		sub  *libraryv1.ExternalSubtitle
		want MediaStream
	}{
		{
			&libraryv1.ExternalSubtitle{Format: "srt", Language: "pt-BR"},
			MediaStream{Language: "por", DisplayTitle: "Portuguese (Brazil) - SRT - External"},
		},
		{
			&libraryv1.ExternalSubtitle{Format: "srt", Language: "en", Forced: true, HearingImpaired: true, Title: "Commentary"},
			MediaStream{
				Language: "eng", Title: "Commentary", IsForced: true, IsHearingImpaired: true,
				DisplayTitle: "English - Commentary - SRT - Forced - SDH - External",
			},
		},
		{
			&libraryv1.ExternalSubtitle{Format: "srt"},
			MediaStream{DisplayTitle: "Unknown - SRT - External"},
		},
	} {
		got := externalSubtitleStream("abc", 5, tt.sub)
		want := tt.want
		want.Codec, want.Type, want.Index, want.AudioSpatialFormat = "srt", "Subtitle", 5, "None"
		want.IsExternal, want.IsTextSubtitleStream, want.SupportsExternalStream = true, true, true
		want.DeliveryMethod, want.DeliveryUrl = "External", "/Videos/abc/abc/Subtitles/5/0/Stream.srt"
		if got != want {
			t.Errorf("externalSubtitleStream(%v) = %+v, want %+v", tt.sub, got, want)
		}
	}
}

func TestVideoItemWithExternalSubtitles(t *testing.T) {
	p := &plugin{}
	m := &libraryv1.Movie{
		Id: "0198a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", Path: "Alien.mkv", Title: "Alien",
		MediaInfo: &libraryv1.MediaInfo{Streams: []*libraryv1.Stream{
			{Index: 0, Type: "video", Codec: "h264"},
			{Index: 1, Type: "audio", Codec: "aac"},
		}},
		ExternalSubtitles: []*libraryv1.ExternalSubtitle{
			{Id: "a", FileId: "f1", Format: "srt", Language: "en"},
			{Id: "b", FileId: "f2", Format: "srt", Language: "pt-BR"},
		},
	}
	item := p.movieItem(m, true)
	if !item.HasSubtitles || len(item.MediaStreams) != 4 {
		t.Fatalf("item = %+v", item)
	}
	for i, wantURL := range []string{
		"/Videos/0198a1b2c3d47e5f8a9b0c1d2e3f4a5b/0198a1b2c3d47e5f8a9b0c1d2e3f4a5b/Subtitles/2/0/Stream.srt",
		"/Videos/0198a1b2c3d47e5f8a9b0c1d2e3f4a5b/0198a1b2c3d47e5f8a9b0c1d2e3f4a5b/Subtitles/3/0/Stream.srt",
	} {
		s := item.MediaStreams[2+i]
		if s.Index != 2+i || s.DeliveryUrl != wantURL || !s.IsExternal {
			t.Errorf("stream %d = %+v", 2+i, s)
		}
	}
	if len(item.MediaSources[0].MediaStreams) != 4 {
		t.Errorf("media source streams = %d, want 4", len(item.MediaSources[0].MediaStreams))
	}

	// Without analysis, the external subtitles come first.
	m.MediaInfo = nil
	item = p.movieItem(m, true)
	if len(item.MediaStreams) != 2 || item.MediaStreams[0].Index != 0 || item.MediaStreams[1].Index != 1 {
		t.Errorf("streams without media info = %+v", item.MediaStreams)
	}
	if !item.HasSubtitles {
		t.Error("HasSubtitles not set")
	}
	if got := externalSubtitleBase(nil); got != 0 {
		t.Errorf("externalSubtitleBase(nil) = %d", got)
	}
}

func TestSubtitleConversion(t *testing.T) {
	for _, tt := range []struct {
		original, format string
		want             subtitleConversion
		ok               bool
	}{
		{"srt", "srt", subtitlePassthrough, true},
		{"srt", "subrip", subtitlePassthrough, true},
		{"srt", "vtt", subtitleVTT, true},
		{"srt", "js", subtitleJSON, true},
		{"srt", "json", subtitleJSON, true},
		{"srt", "ass", 0, false},
		{"ass", "ass", subtitlePassthrough, true},
		{"ass", "vtt", 0, false},
		{"ssa", "ssa", subtitlePassthrough, true},
		{"vtt", "vtt", subtitlePassthrough, true},
		{"vtt", "srt", 0, false},
		{"srt", "txt", 0, false},
	} {
		got, ok := subtitleConvert(tt.original, tt.format)
		if got != tt.want || ok != tt.ok {
			t.Errorf("subtitleConvert(%q, %q) = %v, %v; want %v, %v",
				tt.original, tt.format, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSubtitleDeliveryFormat(t *testing.T) {
	profiles := func(formats ...string) []SubtitleProfile {
		out := make([]SubtitleProfile, len(formats))
		for i, f := range formats {
			out[i] = SubtitleProfile{Format: f, Method: "External"}
		}
		return out
	}
	for name, tt := range map[string]struct {
		profiles []SubtitleProfile
		original string
		want     string
		ok       bool
	}{
		"no profiles":        {nil, "srt", "srt", true},
		"plays the original": {profiles("srt"), "srt", "srt", true},
		"converts to vtt":    {profiles("vtt"), "srt", "vtt", true},
		"vtt before srt":     {profiles("ass", "vtt", "srt"), "srt", "srt", true},
		"ass as it is":       {profiles("ass", "vtt"), "ass", "ass", true},
		"ass no conversion":  {profiles("vtt"), "ass", "", false},
		"vtt as it is":       {profiles("vtt"), "vtt", "vtt", true},
	} {
		got, ok := subtitleDeliveryFormat(tt.profiles, tt.original, "eng")
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: subtitleDeliveryFormat = %q, %v; want %q, %v", name, got, ok, tt.want, tt.ok)
		}
	}

	// A profile that only delivers embedded subtitles is not used.
	embedded := []SubtitleProfile{{Format: "srt", Method: "Embed"}}
	if got, ok := subtitleDeliveryFormat(embedded, "srt", "eng"); got != "" || ok {
		t.Errorf("embedded profile = %q, %v; want no format", got, ok)
	}
}

func TestSRTToVTT(t *testing.T) {
	for name, tt := range map[string]struct{ in, want string }{
		"basic": {
			"1\n00:00:01,000 --> 00:00:02,500\nHello, world\n\n2\n00:00:03,000 --> 00:00:04,000\nBye\n",
			"WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.500\nHello, world\n\n2\n00:00:03.000 --> 00:00:04.000\nBye\n",
		},
		"crlf and bom": {
			"\xef\xbb\xbf1\r\n00:00:01,000 --> 00:00:02,000\r\nHi\r\n",
			"WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nHi\n",
		},
		"latin1": {
			"1\n00:00:01,000 --> 00:00:02,000\nA\xe7\xe3o\n",
			"WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nAção\n",
		},
		"utf8":  {"1\n00:00:01,000 --> 00:00:02,000\nAção\n", "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nAção\n"},
		"empty": {"", "WEBVTT\n"},
	} {
		if got := string(srtToVTT([]byte(tt.in))); got != tt.want {
			t.Errorf("%s: srtToVTT = %q, want %q", name, got, tt.want)
		}
	}
}

func TestSRTToTrackInfo(t *testing.T) {
	for name, tt := range map[string]struct {
		in   string
		want []subtitleTrackEvent
	}{
		"basic": {
			"1\n00:00:01,000 --> 00:00:02,500\nHello, world\n\n2\n00:00:03,000 --> 00:00:04,000\nBye\n",
			[]subtitleTrackEvent{
				{ID: "1", Text: "Hello, world", StartPositionTicks: 10_000_000, EndPositionTicks: 25_000_000},
				{ID: "2", Text: "Bye", StartPositionTicks: 30_000_000, EndPositionTicks: 40_000_000},
			},
		},
		"multiline and position": {
			"1\n00:00:01,500 --> 00:00:02,000 X1:10 X2:20\nfirst\nsecond\n",
			[]subtitleTrackEvent{
				{ID: "1", Text: "first\nsecond", StartPositionTicks: 15_000_000, EndPositionTicks: 20_000_000},
			},
		},
		"no index": {
			"00:00:00,000 --> 00:00:01,000\nHi\n",
			[]subtitleTrackEvent{
				{ID: "1", Text: "Hi", StartPositionTicks: 0, EndPositionTicks: 10_000_000},
			},
		},
		"empty": {"", []subtitleTrackEvent{}},
	} {
		got := srtToTrackInfo([]byte(tt.in))
		if len(got.TrackEvents) != len(tt.want) {
			t.Errorf("%s: got %+v, want %+v", name, got.TrackEvents, tt.want)
			continue
		}
		for i := range tt.want {
			if got.TrackEvents[i] != tt.want[i] {
				t.Errorf("%s: event %d = %+v, want %+v", name, i, got.TrackEvents[i], tt.want[i])
			}
		}
	}
}
