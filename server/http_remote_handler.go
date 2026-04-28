package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/evcc-io/evcc/server/remote"
)

// remoteClientsHandler returns the list of configured remote clients.
func remoteClientsHandler(r *remote.Remote) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		jsonWrite(w, r.Clients())
	}
}

// createRemoteClientHandler creates a new remote client.
func createRemoteClientHandler(r *remote.Remote) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Username  string `json:"username"`
			ExpiresIn int    `json:"expiresIn"` // seconds; 0 = never
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			jsonError(w, http.StatusBadRequest, err)
			return
		}

		var dur time.Duration
		if body.ExpiresIn > 0 {
			dur = time.Duration(body.ExpiresIn) * time.Second
		}

		client, password, err := r.CreateClient(body.Username, dur)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err)
			return
		}

		jsonWrite(w, struct {
			remote.Client
			Password string `json:"password"`
		}{client, password})
	}
}

// deleteRemoteClientHandler removes a remote client.
func deleteRemoteClientHandler(r *remote.Remote) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		username := req.URL.Query().Get("username")
		if username == "" {
			jsonError(w, http.StatusBadRequest, nil)
			return
		}
		if err := r.DeleteClient(username); err != nil {
			jsonError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

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
