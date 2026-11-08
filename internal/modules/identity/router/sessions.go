package router

import (
	"net/http"

	"github.com/lukerops/mserver/internal/modules/identity/service"
)

func (rt *router) authenticate(w http.ResponseWriter, r *http.Request) {
	var req AuthenticateRequestV1
	if !decode(w, r, &req) {
		return
	}
	res, err := rt.svc.Authenticate(r.Context(), service.AuthenticateRequest{
		Login:    req.Login,
		Password: req.Password,
		Device: service.DeviceInfo{
			Identifier: req.Device.Identifier,
			Name:       req.Device.Name,
			Client:     req.Device.Client,
			Version:    req.Device.Version,
		},
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, AuthenticateResponseV1{
		Session: toSessionV1(res.Session),
		Token:   res.Token,
	})
}

func (rt *router) validateToken(w http.ResponseWriter, r *http.Request) {
	var req ValidateTokenRequestV1
	if !decode(w, r, &req) {
		return
	}
	session, err := rt.svc.ValidateToken(r.Context(), req.Token)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSessionV1(session))
}

func (rt *router) selectProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req SelectProfileRequestV1
	if !decode(w, r, &req) {
		return
	}
	session, err := rt.svc.SelectProfile(r.Context(), id, req.ProfileID)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSessionV1(session))
}

func (rt *router) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := rt.svc.RevokeSession(r.Context(), id); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
