package jellyfin

import (
	"testing"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
)

func TestDisplayTitle(t *testing.T) {
	for _, tt := range []struct {
		stream *libraryv1.Stream
		want   string
	}{
		{&libraryv1.Stream{Type: "video", Codec: "h264", Width: 1920, Height: 1080}, "1080p H264 SDR"},
		{&libraryv1.Stream{Type: "video", Codec: "hevc", Width: 3840, Height: 1600, ColorTransfer: "smpte2084"}, "4K HEVC HDR"},
		{&libraryv1.Stream{Type: "video", Codec: "mpeg2video", Width: 720, Height: 480, Interlaced: true}, "480i MPEG2VIDEO SDR"},
		{&libraryv1.Stream{Type: "audio", Codec: "aac", Language: "eng", Channels: 2, Default: true}, "English - AAC - Stereo - Default"},
		{&libraryv1.Stream{Type: "audio", Codec: "eac3", Language: "xyz", Channels: 6}, "XYZ - EAC3 - 5.1"},
		{&libraryv1.Stream{Type: "audio", Codec: "dts", Title: "Commentary", Channels: 3, ChannelLayout: "2.1"}, "Commentary - DTS - 2.1"},
		{&libraryv1.Stream{Type: "subtitle", Codec: "subrip", Language: "por", Forced: true}, "Portuguese - SUBRIP - Forced"},
		{&libraryv1.Stream{Type: "subtitle", Codec: "hdmv_pgs_subtitle", Language: "und"}, "HDMV_PGS_SUBTITLE"},
	} {
		if got := mediaStream(tt.stream).DisplayTitle; got != tt.want {
			t.Errorf("displayTitle(%v) = %q, want %q", tt.stream, got, tt.want)
		}
	}
}

func TestMovieItem(t *testing.T) {
	p := &plugin{}
	m := &libraryv1.Movie{
		Id:        "0198a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
		LibraryId: "0198a1b2-c3d4-7e5f-8a9b-000000000001",
		Path:      "Alien (1979)/Alien.MKV",
		Title:     "Alien",
		SortTitle: "alien",
		Year:      1979,
		MediaInfo: &libraryv1.MediaInfo{
			DurationMs: 1500, Size: 100, BitRate: 2000,
			Streams: []*libraryv1.Stream{
				{Index: 0, Type: "video", Codec: "h264", Width: 1280, Height: 720},
				{Index: 1, Type: "audio", Codec: "aac"},
				{Index: 2, Type: "audio", Codec: "ac3", Default: true},
				{Index: 3, Type: "subtitle", Codec: "subrip"},
			},
		},
	}

	brief := p.movieItem(m, false)
	if brief.MediaSources != nil || brief.MediaStreams != nil {
		t.Errorf("brief item has media sources: %+v", brief)
	}
	item := p.movieItem(m, true)
	if item.Id != "0198a1b2c3d47e5f8a9b0c1d2e3f4a5b" || item.ParentId != "0198a1b2c3d47e5f8a9b000000000001" ||
		item.Type != "Movie" || item.RunTimeTicks != 15_000_000 || item.Container != "mkv" ||
		item.ProductionYear != 1979 || !item.IsHD || !item.HasSubtitles || item.Width != 1280 ||
		item.UserData.Key != item.Id || len(item.MediaStreams) != 4 {
		t.Fatalf("item = %+v", item)
	}
	src := item.MediaSources[0]
	if src.Id != item.Id || src.Protocol != "File" || src.RunTimeTicks != item.RunTimeTicks ||
		src.Size != 100 || src.Bitrate != 2000 || src.Name != "Alien" ||
		src.DefaultAudioStreamIndex == nil || *src.DefaultAudioStreamIndex != 2 {
		t.Errorf("media source = %+v", src)
	}
	if sub := item.MediaStreams[3]; sub.Type != "Subtitle" || !sub.IsTextSubtitleStream {
		t.Errorf("subtitle = %+v", sub)
	}

	// Not analyzed yet: a media source without streams.
	m.MediaInfo = nil
	item = p.movieItem(m, true)
	if item.RunTimeTicks != 0 || len(item.MediaSources) != 1 || item.MediaSources[0].MediaStreams == nil {
		t.Errorf("item without media info = %+v", item)
	}
}

func TestMovieSort(t *testing.T) {
	for _, tt := range []struct {
		sortBy, sortOrder string
		by                string
		desc              bool
	}{
		{"", "", "title", false},
		{"SortName", "Ascending", "title", false},
		{"DateCreated,SortName", "Descending", "added", true},
		{"ProductionYear,PremiereDate,SortName", "Descending,Ascending", "year", true},
		{"Random", "", "title", false},
	} {
		by, desc := movieSort(tt.sortBy, tt.sortOrder)
		if by != tt.by || desc != tt.desc {
			t.Errorf("movieSort(%q, %q) = %q, %v", tt.sortBy, tt.sortOrder, by, desc)
		}
	}
}
