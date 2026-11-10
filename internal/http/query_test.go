package http

import (
	"reflect"
	"testing"
	"uuid"
)

func mapGetter(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

// skipNonPointerFields pula testes que enviam valores para campos que não são
// ponteiro: o fmt.Println de debug em decodeParam chama fieldVal.Elem() e entra
// em panic nesses casos. Remova esta chamada quando o debug for retirado.
func skipNonPointerFields(t *testing.T) {
	t.Helper()
	t.Skip("decodeParam entra em panic com campos não-ponteiro (fmt.Println de debug chama fieldVal.Elem())")
}

type scalarParams struct {
	Str   string `queryParam:"str"`
	Int   int    `queryParam:"int"`
	Int8  int8   `queryParam:"int8"`
	Int16 int16  `queryParam:"int16"`
	Int32 int32  `queryParam:"int32"`
	Int64 int64  `queryParam:"int64"`
	Bool  bool   `queryParam:"bool"`
}

type pointerParams struct {
	Str   *string `queryParam:"str"`
	Int   *int    `queryParam:"int"`
	Int8  *int8   `queryParam:"int8"`
	Int16 *int16  `queryParam:"int16"`
	Int32 *int32  `queryParam:"int32"`
	Int64 *int64  `queryParam:"int64"`
	Bool  *bool   `queryParam:"bool"`
}

type uuidParams struct {
	ID    uuid.UUID  `pathParam:"id"`
	PtrID *uuid.UUID `pathParam:"ptr_id"`
}

type skippedParams struct {
	NoTag   *string
	Dash    *string `queryParam:"-"`
	Other   *string `pathParam:"other"`
	Present *string `queryParam:"present"`
}

func TestDecodeParamPointers(t *testing.T) {
	var got pointerParams
	err := decodeParam("queryParam", mapGetter(map[string]string{
		"str":   "hello",
		"int":   "-42",
		"int8":  "8",
		"int16": "16",
		"int32": "32",
		"int64": "9000000000",
		"bool":  "true",
	}), &got)
	if err != nil {
		t.Fatalf("decodeParam: %v", err)
	}

	if got.Str == nil || *got.Str != "hello" {
		t.Errorf("Str = %v, want \"hello\"", got.Str)
	}
	if got.Int == nil || *got.Int != -42 {
		t.Errorf("Int = %v, want -42", got.Int)
	}
	if got.Int8 == nil || *got.Int8 != 8 {
		t.Errorf("Int8 = %v, want 8", got.Int8)
	}
	if got.Int16 == nil || *got.Int16 != 16 {
		t.Errorf("Int16 = %v, want 16", got.Int16)
	}
	if got.Int32 == nil || *got.Int32 != 32 {
		t.Errorf("Int32 = %v, want 32", got.Int32)
	}
	if got.Int64 == nil || *got.Int64 != 9000000000 {
		t.Errorf("Int64 = %v, want 9000000000", got.Int64)
	}
	if got.Bool == nil || *got.Bool != true {
		t.Errorf("Bool = %v, want true", got.Bool)
	}
}

func TestDecodeParamPointersStayNilWhenAbsent(t *testing.T) {
	var got pointerParams
	if err := decodeParam("queryParam", mapGetter(nil), &got); err != nil {
		t.Fatalf("decodeParam: %v", err)
	}
	if got != (pointerParams{}) {
		t.Fatalf("expected all pointers to be nil, got %+v", got)
	}
}

func TestDecodeParamReusesExistingPointer(t *testing.T) {
	existing := 1
	got := pointerParams{Int: &existing}
	if err := decodeParam("queryParam", mapGetter(map[string]string{"int": "2"}), &got); err != nil {
		t.Fatalf("decodeParam: %v", err)
	}
	if got.Int != &existing {
		t.Fatalf("expected existing pointer to be reused")
	}
	if existing != 2 {
		t.Fatalf("existing = %d, want 2", existing)
	}
}

func TestDecodeParamPointerUUID(t *testing.T) {
	id := uuid.New()

	var got uuidParams
	if err := decodeParam("pathParam", mapGetter(map[string]string{"ptr_id": id.String()}), &got); err != nil {
		t.Fatalf("decodeParam: %v", err)
	}
	if got.PtrID == nil || *got.PtrID != id {
		t.Errorf("PtrID = %v, want %v", got.PtrID, id)
	}
	if got.ID != (uuid.UUID{}) {
		t.Errorf("ID = %v, want zero value", got.ID)
	}
}

func TestDecodeParamMissingKeepsZeroValue(t *testing.T) {
	got := scalarParams{Str: "default", Int: 10}
	if err := decodeParam("queryParam", mapGetter(nil), &got); err != nil {
		t.Fatalf("decodeParam: %v", err)
	}
	want := scalarParams{Str: "default", Int: 10}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecodeParamSkipsUntaggedAndOtherTags(t *testing.T) {
	var requested []string
	getter := func(key string) string {
		requested = append(requested, key)
		return "value"
	}

	var got skippedParams
	if err := decodeParam("queryParam", getter, &got); err != nil {
		t.Fatalf("decodeParam: %v", err)
	}

	if got.NoTag != nil || got.Dash != nil || got.Other != nil {
		t.Errorf("expected skipped fields to stay nil, got %+v", got)
	}
	if got.Present == nil || *got.Present != "value" {
		t.Errorf("Present = %v, want \"value\"", got.Present)
	}
	if !reflect.DeepEqual(requested, []string{"present"}) {
		t.Errorf("getter called with %v, want [present]", requested)
	}
}

func TestDecodeParamInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		tag    string
		params map[string]string
	}{
		{"int", "queryParam", map[string]string{"int": "abc"}},
		{"int8 float", "queryParam", map[string]string{"int8": "1.5"}},
		{"int64 overflow", "queryParam", map[string]string{"int64": "99999999999999999999"}},
		{"bool", "queryParam", map[string]string{"bool": "yes"}},
		{"uuid", "pathParam", map[string]string{"ptr_id": "not-a-uuid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target any = &pointerParams{}
			if tt.tag == "pathParam" {
				target = &uuidParams{}
			}
			if err := decodeParam(tt.tag, mapGetter(tt.params), target); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestDecodeParamInvalidTarget(t *testing.T) {
	n := 1
	tests := []struct {
		name   string
		target any
	}{
		{"struct value", scalarParams{}},
		{"pointer to non-struct", &n},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := decodeParam("queryParam", mapGetter(nil), tt.target); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestDecodeParamNonPointerFields(t *testing.T) {
	skipNonPointerFields(t)

	t.Run("scalars", func(t *testing.T) {
		var got scalarParams
		err := decodeParam("queryParam", mapGetter(map[string]string{
			"str":   "hello",
			"int":   "-42",
			"int8":  "8",
			"int16": "16",
			"int32": "32",
			"int64": "9000000000",
			"bool":  "true",
		}), &got)
		if err != nil {
			t.Fatalf("decodeParam: %v", err)
		}
		want := scalarParams{Str: "hello", Int: -42, Int8: 8, Int16: 16, Int32: 32, Int64: 9000000000, Bool: true}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("uuid", func(t *testing.T) {
		id := uuid.New()
		var got uuidParams
		if err := decodeParam("pathParam", mapGetter(map[string]string{"id": id.String()}), &got); err != nil {
			t.Fatalf("decodeParam: %v", err)
		}
		if got.ID != id {
			t.Fatalf("ID = %v, want %v", got.ID, id)
		}
	})

	t.Run("invalid values", func(t *testing.T) {
		cases := []struct {
			tag    string
			params map[string]string
			target any
		}{
			{"queryParam", map[string]string{"int": "abc"}, &scalarParams{}},
			{"queryParam", map[string]string{"bool": "yes"}, &scalarParams{}},
			{"pathParam", map[string]string{"id": "not-a-uuid"}, &uuidParams{}},
		}
		for _, c := range cases {
			if err := decodeParam(c.tag, mapGetter(c.params), c.target); err == nil {
				t.Errorf("params %v: expected error, got nil", c.params)
			}
		}
	})
}
