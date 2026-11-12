package handlers

import "testing"

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
