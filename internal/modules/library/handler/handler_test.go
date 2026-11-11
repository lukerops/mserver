package handler

import (
	"testing"

	"connectrpc.com/connect/v2"
)

func TestPage(t *testing.T) {
	for _, tt := range []struct {
		name          string
		limit, offset int32
		wantLimit     int
		wantOffset    int
		wantErr       bool
	}{
		{name: "defaults", wantLimit: maxListLimit},
		{name: "within the maximum", limit: 10, offset: 5, wantLimit: 10, wantOffset: 5},
		{name: "the maximum", limit: maxListLimit, wantLimit: maxListLimit},
		{name: "above the maximum", limit: maxListLimit + 1, wantLimit: maxListLimit},
		{name: "negative limit", limit: -1, wantErr: true},
		{name: "negative offset", offset: -1, wantErr: true},
	} {
		limit, offset, err := page(tt.limit, tt.offset)
		if tt.wantErr {
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("%s: got %v, want InvalidArgument", tt.name, err)
			}
			continue
		}
		if err != nil || limit != tt.wantLimit || offset != tt.wantOffset {
			t.Errorf("%s: page = %d, %d, %v; want %d, %d", tt.name, limit, offset, err, tt.wantLimit, tt.wantOffset)
		}
	}
}

func TestParseOptionalID(t *testing.T) {
	if id, err := parseOptionalID("x", ""); err != nil || id.String() != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("empty id = %v, %v", id, err)
	}
	if _, err := parseOptionalID("x", "nope"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("invalid id: got %v, want InvalidArgument", err)
	}
}
