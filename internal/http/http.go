package http

import (
	"encoding/json"
	"errors"
	"net/http"
)

func parseRequest[P, B any](r *http.Request, params *P, body *B) error {
	// Processa e injeta os Query Params se 'Q' não for struct{}
	if any(params) != any(struct{}{}) {
		if err := decodeParam("queryParam", r.URL.Query().Get, params); err != nil {
			return err
		}

		if err := decodeParam("pathParam", r.PathValue, params); err != nil {
			return err
		}
	}

	if any(body) != any(struct{}{}) {
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			return errors.New("only json is allowed")
		}

		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			return err
		}
	}

	return nil
}

type HandlerWithResponse[P, B, R any] func(r *http.Request, params *P, body *B) (status int, reponse *R, err error)
type HandlerWithoutResponse[P, B any] func(r *http.Request, params *P, body *B) (status int, err error)

func WithResponse[P, B, R any](handler HandlerWithResponse[P, B, R]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var params P
		var body B

		if err := parseRequest(r, &params, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Executa a lógica de negócio do seu handler passando tudo mastigado
		status, output, err := handler(r, &params, &body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if output != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(output)
		}

		w.WriteHeader(status)
	}
}

func WithoutResponse[P, B any](h HandlerWithoutResponse[P, B]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var params P
		var body B

		if err := parseRequest(r, &params, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Executa a lógica de negócio do seu handler passando tudo mastigado
		status, err := h(r, &params, &body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(status)
	}
}
