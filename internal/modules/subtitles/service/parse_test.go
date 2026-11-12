package service

import "testing"

func TestParseTags(t *testing.T) {
	tests := []struct {
		rest string
		want tags
	}{
		{"", tags{}},
		{"en", tags{Language: "en"}},
		{"EN", tags{Language: "en"}},
		{"eng", tags{Language: "eng"}},
		{"pt-BR", tags{Language: "pt-BR"}},
		{"pt-br", tags{Language: "pt-BR"}},
		{"pt_BR", tags{Language: "pt-BR"}},
		{"zh-hant-tw", tags{Language: "zh-Hant-TW"}},
		{"es-419", tags{Language: "es-419"}},
		{"en.forced", tags{Language: "en", Forced: true}},
		{"forced.en", tags{Language: "en", Forced: true}},
		{"forced", tags{Forced: true}},
		{"FORCED", tags{Forced: true}},
		{"en.sdh", tags{Language: "en", HearingImpaired: true}},
		{"en.hi", tags{Language: "en", HearingImpaired: true}},
		{"en.cc", tags{Language: "en", HearingImpaired: true}},
		{"en.forced.sdh", tags{Language: "en", Forced: true, HearingImpaired: true}},
		{"Commentary", tags{Title: "Commentary"}},
		{"en.Director Cut", tags{Language: "en", Title: "Director Cut"}},
		{"Commentary.en", tags{Language: "en", Title: "Commentary"}},
		{"en.fr", tags{Language: "en", Title: "fr"}},
		{"pt-BR.", tags{Language: "pt-BR"}},
		{".en", tags{Language: "en"}},
		{"e", tags{Title: "e"}},
		{"english", tags{Title: "english"}},
	}
	for _, tt := range tests {
		if got := parseTags(tt.rest); got != tt.want {
			t.Errorf("parseTags(%q) = %+v, want %+v", tt.rest, got, tt.want)
		}
	}
}

func TestTagsAfterStem(t *testing.T) {
	tests := []struct {
		video, subtitle string
		rest            string
		ok              bool
	}{
		{"Movie", "Movie", "", true},
		{"Movie", "movie", "", true},
		{"Movie", "Movie.pt-BR", "pt-BR", true},
		{"Movie", "MOVIE.EN.Forced", "EN.Forced", true},
		{"Movie (2020)", "Movie (2020).en", "en", true},
		{"Movie", "Movie2", "", false},
		{"Movie", "Movie 2.en", "", false},
		{"Movie", "Mov", "", false},
		{"Movie", "Other.en", "", false},
		{"", "Movie", "", false},
		{"Filme Ação", "filme ação.pt", "pt", true},
	}
	for _, tt := range tests {
		rest, ok := tagsAfterStem(tt.video, tt.subtitle)
		if rest != tt.rest || ok != tt.ok {
			t.Errorf("tagsAfterStem(%q, %q) = %q, %v; want %q, %v", tt.video, tt.subtitle, rest, ok, tt.rest, tt.ok)
		}
	}
}

func TestStemOf(t *testing.T) {
	for p, want := range map[string]string{
		"Movie.mkv":              "Movie",
		"Dir/Sub/Movie.en.srt":   "Movie.en",
		"Dir/Movie":              "Movie",
		"Dir/.mkv":               "",
		"Movie (2020)/Movie.mp4": "Movie",
	} {
		if got := stemOf(p); got != want {
			t.Errorf("stemOf(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestSubtitleFormat(t *testing.T) {
	if f, ok := subtitleFormat("application/x-subrip"); !ok || f != "srt" {
		t.Errorf("subtitleFormat(srt) = %q, %v", f, ok)
	}
	for _, m := range []string{"", "video/x-matroska", "text/vtt", "application/octet-stream"} {
		if _, ok := subtitleFormat(m); ok {
			t.Errorf("subtitleFormat(%q) = true, want false", m)
		}
	}
}
