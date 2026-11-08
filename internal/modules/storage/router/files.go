package router

import "net/http"

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

func (rt *router) resolve(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := rt.svc.Resolve(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ResolveResponseV1{URL: u})
}
