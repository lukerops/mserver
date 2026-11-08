package router

import "net/http"

func (rt *router) getAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	account, err := rt.svc.GetAccount(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAccountV1(account))
}

func (rt *router) listProfiles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	profiles, err := rt.svc.ListProfiles(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := ProfileListV1{Profiles: make([]ProfileV1, len(profiles))}
	for i, p := range profiles {
		resp.Profiles[i] = toProfileV1(p)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	profile, err := rt.svc.GetProfile(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProfileV1(profile))
}
