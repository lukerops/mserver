package service

import "testing"

func TestMimeType(t *testing.T) {
	for _, tt := range []struct{ path, want string }{
		{"Alien (1979)/Alien.mkv", "video/x-matroska"},
		{"movie.MP4", "video/mp4"},
		{"a/b/clip.ts", "video/mp2t"},
		{"old.avi", "video/x-msvideo"},
		{"Alien.en.srt", "application/x-subrip"},
		{"Alien.vtt", "text/vtt"},
		{"song.mka", "audio/x-matroska"},
		{"poster.jpg", "image/jpeg"},
		{"README", DefaultMimeType},
		{"file.unknownext", DefaultMimeType},
		{".mkv", "video/x-matroska"},
	} {
		if got := MimeType(tt.path); got != tt.want {
			t.Errorf("MimeType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
