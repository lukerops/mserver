package router

import (
	"net/http"

	"github.com/lukerops/mserver/internal/modules/library/service"
)

func (rt *router) createLibrary(w http.ResponseWriter, r *http.Request) {
	var req CreateLibraryRequestV1
	if !decode(w, r, &req) {
		return
	}
	lib, err := rt.svc.CreateLibrary(r.Context(), service.CreateLibraryRequest{
		Name:        req.Name,
		MediaType:   req.MediaType,
		LocationIDs: req.LocationIDs,
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLibraryV1(lib))
}

func (rt *router) listLibraries(w http.ResponseWriter, r *http.Request) {
	libraries, err := rt.svc.ListLibraries(r.Context())
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := LibraryListV1{Libraries: make([]LibraryV1, len(libraries))}
	for i, lib := range libraries {
		resp.Libraries[i] = toLibraryV1(lib)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	lib, err := rt.svc.GetLibrary(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toLibraryV1(lib))
}

func (rt *router) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := rt.svc.DeleteLibrary(r.Context(), id); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) resyncLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := rt.svc.ResyncLibrary(r.Context(), id); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
