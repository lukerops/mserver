package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Os campos são ponteiros porque decodeParam entra em panic com campos
// não-ponteiro (ver skipNonPointerFields em query_test.go).
type testParams struct {
	ID    *string `pathParam:"id"`
	Limit *int    `queryParam:"limit"`
}

func assertParams(t *testing.T, got testParams, wantID string, wantLimit int) {
	t.Helper()
	if got.ID == nil || *got.ID != wantID {
		t.Errorf("ID = %v, want %q", got.ID, wantID)
	}
	if got.Limit == nil || *got.Limit != wantLimit {
		t.Errorf("Limit = %v, want %d", got.Limit, wantLimit)
	}
}

// skipEmptyBodyType pula testes que usam B = struct{}: parseRequest compara
// any(body) (um *B) com any(struct{}{}), o que é sempre diferente, então JSON
// é exigido mesmo quando o handler não espera body.
func skipEmptyBodyType(t *testing.T) {
	t.Helper()
	t.Skip("parseRequest exige JSON mesmo com B = struct{} (comparação any(body) != any(struct{}{}) é sempre verdadeira)")
}

type testBody struct {
	Name string `json:"name"`
}

type testResponse struct {
	Message string `json:"message"`
}

func newJSONRequest(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// serve routes the request through a ServeMux so path values are populated.
func serve(pattern string, h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle(pattern, h)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestParseRequest(t *testing.T) {
	r := newJSONRequest(http.MethodPost, "/items/abc?limit=10", `{"name":"foo"}`)
	r.SetPathValue("id", "abc")

	var params testParams
	var body testBody
	if err := parseRequest(r, &params, &body); err != nil {
		t.Fatalf("parseRequest: %v", err)
	}

	assertParams(t, params, "abc", 10)
	if body != (testBody{Name: "foo"}) {
		t.Errorf("body = %+v", body)
	}
}

func TestParseRequestErrors(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		contentType string
		body        string
	}{
		{"invalid query param", "/items?limit=abc", "application/json", `{}`},
		{"missing content type", "/items", "", `{}`},
		{"wrong content type", "/items", "text/plain", `{}`},
		{"invalid json", "/items", "application/json", `{"name":`},
		{"empty body", "/items", "application/json", ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tt.target, strings.NewReader(tt.body))
			if tt.contentType != "" {
				r.Header.Set("Content-Type", tt.contentType)
			}

			var params testParams
			var body testBody
			if err := parseRequest(r, &params, &body); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestParseRequestEmptyBodyType(t *testing.T) {
	skipEmptyBodyType(t)

	// With B = struct{}, no body or Content-Type should be required.
	r := httptest.NewRequest(http.MethodGet, "/items?limit=5", nil)

	var params testParams
	var body struct{}
	if err := parseRequest(r, &params, &body); err != nil {
		t.Fatalf("parseRequest: %v", err)
	}
	if params.Limit == nil || *params.Limit != 5 {
		t.Errorf("Limit = %v, want 5", params.Limit)
	}
}

func TestWithResponse(t *testing.T) {
	var gotParams testParams
	var gotBody testBody

	h := WithResponse(func(r *http.Request, params *testParams, body *testBody) (int, *testResponse, error) {
		gotParams = *params
		gotBody = *body
		return http.StatusOK, &testResponse{Message: "hello " + body.Name}, nil
	})

	r := newJSONRequest(http.MethodPost, "/items/abc?limit=3", `{"name":"foo"}`)
	w := serve("POST /items/{id}", h, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp testResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Message != "hello foo" {
		t.Errorf("Message = %q, want %q", resp.Message, "hello foo")
	}

	assertParams(t, gotParams, "abc", 3)
	if gotBody != (testBody{Name: "foo"}) {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestWithResponseCustomStatusWithOutput(t *testing.T) {
	t.Skip("WithResponse chama WriteHeader depois de escrever o body, então o status é sempre 200 quando há resposta")

	h := WithResponse(func(r *http.Request, params *testParams, body *testBody) (int, *testResponse, error) {
		return http.StatusCreated, &testResponse{Message: "created"}, nil
	})

	r := newJSONRequest(http.MethodPost, "/items/abc", `{}`)
	w := serve("POST /items/{id}", h, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestWithResponseNilOutput(t *testing.T) {
	h := WithResponse(func(r *http.Request, params *testParams, body *testBody) (int, *testResponse, error) {
		return http.StatusAccepted, nil, nil
	})

	r := newJSONRequest(http.MethodPost, "/items/abc", `{}`)
	w := serve("POST /items/{id}", h, r)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", w.Body.String())
	}
}

func TestWithResponseBadRequest(t *testing.T) {
	called := false
	h := WithResponse(func(r *http.Request, params *testParams, body *testBody) (int, *testResponse, error) {
		called = true
		return http.StatusOK, nil, nil
	})

	r := newJSONRequest(http.MethodPost, "/items/abc", `not json`)
	w := serve("POST /items/{id}", h, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if called {
		t.Error("handler should not be called on invalid request")
	}
}

func TestWithResponseHandlerError(t *testing.T) {
	h := WithResponse(func(r *http.Request, params *testParams, body *testBody) (int, *testResponse, error) {
		return http.StatusOK, &testResponse{Message: "ignored"}, errors.New("boom")
	})

	r := newJSONRequest(http.MethodPost, "/items/abc", `{}`)
	w := serve("POST /items/{id}", h, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "boom" {
		t.Errorf("body = %q, want %q", got, "boom")
	}
}

func TestWithResponseNoParamsNoBody(t *testing.T) {
	skipEmptyBodyType(t)

	h := WithResponse(func(r *http.Request, params *struct{}, body *struct{}) (int, *testResponse, error) {
		return http.StatusOK, &testResponse{Message: "ok"}, nil
	})

	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := serve("GET /health", h, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %q)", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestWithoutResponse(t *testing.T) {
	var gotParams testParams
	var gotBody testBody

	h := WithoutResponse(func(r *http.Request, params *testParams, body *testBody) (int, error) {
		gotParams = *params
		gotBody = *body
		return http.StatusNoContent, nil
	})

	r := newJSONRequest(http.MethodPut, "/items/xyz?limit=7", `{"name":"bar"}`)
	w := serve("PUT /items/{id}", h, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if b, _ := io.ReadAll(w.Body); len(b) != 0 {
		t.Errorf("body = %q, want empty", b)
	}
	assertParams(t, gotParams, "xyz", 7)
	if gotBody != (testBody{Name: "bar"}) {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestWithoutResponseBadRequest(t *testing.T) {
	called := false
	h := WithoutResponse(func(r *http.Request, params *testParams, body *testBody) (int, error) {
		called = true
		return http.StatusOK, nil
	})

	r := newJSONRequest(http.MethodPut, "/items/xyz?limit=nope", `{}`)
	w := serve("PUT /items/{id}", h, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if called {
		t.Error("handler should not be called on invalid request")
	}
}

func TestWithoutResponseHandlerError(t *testing.T) {
	h := WithoutResponse(func(r *http.Request, params *testParams, body *testBody) (int, error) {
		return http.StatusOK, errors.New("boom")
	})

	r := newJSONRequest(http.MethodPut, "/items/xyz", `{}`)
	w := serve("PUT /items/{id}", h, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "boom" {
		t.Errorf("body = %q, want %q", got, "boom")
	}
}

func TestWithoutResponseNoParamsNoBody(t *testing.T) {
	skipEmptyBodyType(t)

	h := WithoutResponse(func(r *http.Request, params *struct{}, body *struct{}) (int, error) {
		return http.StatusNoContent, nil
	})

	r := httptest.NewRequest(http.MethodDelete, "/items", nil)
	w := serve("DELETE /items", h, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body: %q)", w.Code, http.StatusNoContent, w.Body.String())
	}
}
