package handler

import (
	"log/slog"
	"net/http"
	"path"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/service"
)

// Mux returns the routes of the data plane: the content of the files as
// plain HTTP, outside of ConnectRPC. It is not versioned. GET also serves
// HEAD.
func (h *Handler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /locations/{location_id}/files/{file_id}", h.fileContent)
	return mux
}

// fileContent serves the content of a file. Range requests and conditional
// requests are handled by http.ServeContent.
func (h *Handler) fileContent(w http.ResponseWriter, r *http.Request) {
	location, err := uuid.Parse(r.PathValue("location_id"))
	if err != nil {
		http.Error(w, "invalid location id", http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(r.PathValue("file_id"))
	if err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}
	f, content, err := h.svc.OpenFile(r.Context(), location, id)
	if err != nil {
		h.contentError(w, r, err)
		return
	}
	defer content.Close()

	info, err := content.Stat()
	if err != nil {
		h.contentError(w, r, err)
		return
	}
	// Unknown types are left for http.ServeContent to sniff.
	if typ := service.MimeType(f.Path); typ != service.DefaultMimeType {
		w.Header().Set("Content-Type", typ)
	}
	http.ServeContent(w, r, path.Base(f.Path), info.ModTime(), content)
}

func (h *Handler) contentError(w http.ResponseWriter, r *http.Request, err error) {
	status := httpStatus(err)
	if status == http.StatusInternalServerError {
		h.log.ErrorContext(r.Context(), "storage: request failed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		http.Error(w, "internal error", status)
		return
	}
	http.Error(w, err.Error(), status)
}
