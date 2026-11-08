package jellyfin

import (
	"encoding/json"
	"testing"
)

func TestFlexInt(t *testing.T) {
	for in, want := range map[string]int64{`1`: 1, `"1"`: 1, `"-1"`: -1, `120000000`: 120000000} {
		var n flexInt
		if err := json.Unmarshal([]byte(in), &n); err != nil || int64(n) != want {
			t.Errorf("%s: got %d, %v; want %d", in, n, err, want)
		}
	}
	var p *flexInt
	if err := json.Unmarshal([]byte(`null`), &p); err != nil || p != nil {
		t.Errorf("null: got %v, %v", p, err)
	}
	for _, in := range []string{`"a"`, `1.5`, `true`} {
		var n flexInt
		if err := json.Unmarshal([]byte(in), &n); err == nil {
			t.Errorf("%s: want error", in)
		}
	}
}
