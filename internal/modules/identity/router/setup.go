package router

import (
	"net/http"

	"github.com/lukerops/mserver/internal/modules/identity/service"
)

func (rt *router) setup(w http.ResponseWriter, r *http.Request) {
	var req SetupRequestV1
	if !decode(w, r, &req) {
		return
	}
	res, err := rt.svc.Setup(r.Context(), service.SetupRequest{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		Profile: service.NewProfile{
			Handle:      req.Profile.Handle,
			DisplayName: req.Profile.DisplayName,
		},
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, SetupResponseV1{
		Account: toAccountV1(res.Account),
		Profile: toProfileV1(res.Profile),
	})
}

func (rt *router) resyncAccounts(w http.ResponseWriter, r *http.Request) {
	if err := rt.svc.ResyncAccounts(r.Context()); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) resyncProfiles(w http.ResponseWriter, r *http.Request) {
	if err := rt.svc.ResyncProfiles(r.Context()); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
