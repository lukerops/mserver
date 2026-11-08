package router

import (
	"net/http"

	"github.com/lukerops/mserver/internal/modules/storage/service"
)

func (rt *router) createLocation(w http.ResponseWriter, r *http.Request) {
	var req CreateLocationRequestV1
	if !decode(w, r, &req) {
		return
	}
	loc, err := rt.svc.CreateLocation(r.Context(), service.CreateLocationRequest{
		Driver:  req.Driver,
		Options: req.Options,
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLocationV1(loc))
}

func (rt *router) listLocations(w http.ResponseWriter, r *http.Request) {
	locations, err := rt.svc.ListLocations(r.Context())
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := LocationListV1{Locations: make([]LocationV1, len(locations))}
	for i, loc := range locations {
		resp.Locations[i] = toLocationV1(loc)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	loc, err := rt.svc.GetLocation(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toLocationV1(loc))
}

func (rt *router) deleteLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := rt.svc.DeleteLocation(r.Context(), id); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) scanLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := rt.svc.ScanLocation(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ScanResultV1{
		Added:   res.Added,
		Changed: res.Changed,
		Moved:   res.Moved,
		Removed: res.Removed,
	})
}

func (rt *router) listFiles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	files, err := rt.svc.ListFiles(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := FileListV1{Files: make([]FileV1, len(files))}
	for i, f := range files {
		resp.Files[i] = toFileV1(f)
	}
	writeJSON(w, http.StatusOK, resp)
}
