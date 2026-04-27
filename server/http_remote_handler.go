package server

import (
	"encoding/json"
	"net/http"

	"github.com/evcc-io/evcc/server/remote"
)

// updateRemoteAuthKeyHandler sets the Tailscale auth key.
func updateRemoteAuthKeyHandler(r *remote.Remote) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			AuthKey string `json:"authKey"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			jsonError(w, http.StatusBadRequest, err)
			return
		}
		if err := r.UpdateAuthKey(body.AuthKey); err != nil {
			jsonError(w, http.StatusInternalServerError, err)
			return
		}
		jsonWrite(w, true)
	}
}

// updateRemoteHostnameHandler sets the Tailscale hostname.
func updateRemoteHostnameHandler(r *remote.Remote) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Hostname string `json:"hostname"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			jsonError(w, http.StatusBadRequest, err)
			return
		}
		if err := r.UpdateHostname(body.Hostname); err != nil {
			jsonError(w, http.StatusInternalServerError, err)
			return
		}
		jsonWrite(w, true)
	}
}
