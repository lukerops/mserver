package router

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/google/uuid"
)

func (rt *router) getFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	f, err := rt.svc.GetFile(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toFileV1(f))
}

func (rt *router) listSiblings(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	siblings, err := rt.svc.ListSiblings(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := SiblingListV1{Siblings: make([]SiblingV1, len(siblings))}
	for i, s := range siblings {
		resp.Siblings[i] = SiblingV1{FileID: s.FileID, Path: s.Path}
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolve answers the URL of the content of a file, on the address the
// request was sent to.
func (rt *router) resolve(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := rt.svc.Resolve(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	u := url.URL{Scheme: "http", Host: r.Host, Path: p}
	if r.TLS != nil {
		u.Scheme = "https"
	}
	writeJSON(w, http.StatusOK, ResolveResponseV1{URL: u.String()})
}

// fileContent serves the content of a file (the data plane). Range
// requests and conditional requests are handled by http.ServeContent.
func (rt *router) fileContent(w http.ResponseWriter, r *http.Request) {
	location, err := uuid.Parse(r.PathValue("location_id"))
	if err != nil {
		badRequest(w, r, "invalid location id "+r.PathValue("location_id"))
		return
	}
	id, err := uuid.Parse(r.PathValue("file_id"))
	if err != nil {
		badRequest(w, r, "invalid file id "+r.PathValue("file_id"))
		return
	}
	f, content, err := rt.svc.OpenFile(r.Context(), location, id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	defer content.Close()

	info, err := content.Stat()
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	if typ := contentType(f.Path); typ != "" {
		w.Header().Set("Content-Type", typ)
	}
	http.ServeContent(w, r, path.Base(f.Path), info.ModTime(), content)
}

// contentTypes are the media types of the extensions that
// mime.TypeByExtension does not know on every system. The others are
// guessed by http.ServeContent.
var contentTypes = map[string]string{
	".mkv":  "video/x-matroska",
	".mka":  "audio/x-matroska",
	".webm": "video/webm",
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".ts":   "video/mp2t",
	".m2ts": "video/mp2t",
	".mts":  "video/mp2t",
	".srt":  "application/x-subrip",
	".vtt":  "text/vtt",
	".ass":  "text/x-ssa",
	".ssa":  "text/x-ssa",
}

func contentType(p string) string {
	return contentTypes[strings.ToLower(path.Ext(p))]
}
