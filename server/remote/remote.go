package remote

import (
	"net/http"
	"sync"

	"github.com/evcc-io/evcc/api/globalconfig"
	"github.com/evcc-io/evcc/cmd/shutdown"
	"github.com/evcc-io/evcc/core/keys"
	"github.com/evcc-io/evcc/server/db/settings"
	"github.com/evcc-io/evcc/util"
)

// defaultHostname is the Tailscale hostname used when none is configured.
const defaultHostname = "evcc"

// Settings is the persisted remote access configuration.
type Settings struct {
	Enabled  bool   `json:"enabled"`
	AuthKey  string `json:"authKey,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// Remote manages the Tailscale-based remote access lifecycle.
type Remote struct {
	mu          sync.Mutex
	settings    Settings
	node        *TsNode
	httpHandler http.Handler
	log         *util.Logger
	publisher   chan<- util.Param
}

// New creates a new Remote manager, loads persisted settings, and connects if enabled.
func New(httpHandler http.Handler, valueChan chan<- util.Param) *Remote {
	r := &Remote{
		httpHandler: httpHandler,
		log:         util.NewLogger("remote"),
		publisher:   valueChan,
	}

	// load saved settings
	_ = settings.Json(keys.Remote, &r.settings)

	if r.settings.Enabled {
		if err := r.start(); err != nil {
			r.log.ERROR.Printf("remote access: %v", err)
		}
	}

	shutdown.Register(r.stop)

	return r
}

// Enable enables or disables remote access.
func (r *Remote) Enable(enable bool) error {
	r.mu.Lock()
	r.settings.Enabled = enable
	r.saveSettings()
	r.mu.Unlock()

	if enable {
		if err := r.start(); err != nil {
			return err
		}
	} else {
		r.stop()
	}

	r.publish()
	return nil
}

// Enabled returns whether remote access is enabled.
func (r *Remote) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.settings.Enabled
}

// UpdateAuthKey updates the Tailscale auth key and restarts the node if active.
func (r *Remote) UpdateAuthKey(authKey string) error {
	r.mu.Lock()
	wasEnabled := r.settings.Enabled
	r.settings.AuthKey = authKey
	r.saveSettings()
	r.mu.Unlock()

	if wasEnabled {
		r.stop()
		if err := r.start(); err != nil {
			return err
		}
	}

	r.publish()
	return nil
}

// UpdateHostname updates the Tailscale hostname and restarts the node if active.
func (r *Remote) UpdateHostname(hostname string) error {
	r.mu.Lock()
	wasEnabled := r.settings.Enabled
	r.settings.Hostname = hostname
	r.saveSettings()
	r.mu.Unlock()

	if wasEnabled {
		r.stop()
		if err := r.start(); err != nil {
			return err
		}
	}

	r.publish()
	return nil
}

func (r *Remote) start() error {
	r.mu.Lock()
	authKey := r.settings.AuthKey
	hostname := r.settings.Hostname
	r.mu.Unlock()

	node := NewTsNode(r.log, r.publish)

	r.mu.Lock()
	r.node = node
	r.mu.Unlock()

	return node.Start(StateDir(), authKey, hostname, r.httpHandler)
}

func (r *Remote) stop() {
	r.mu.Lock()
	node := r.node
	r.node = nil
	r.mu.Unlock()

	if node != nil {
		node.Close()
	}
}

// saveSettings persists the current settings. Must be called with mu held.
func (r *Remote) saveSettings() {
	if err := settings.SetJson(keys.Remote, r.settings); err != nil {
		r.log.ERROR.Println(err)
	}
}

// ConfigStatus returns the current remote access config and status.
func (r *Remote) ConfigStatus() globalconfig.ConfigStatus {
	r.mu.Lock()
	node := r.node
	enabled := r.settings.Enabled
	hostname := r.settings.Hostname
	if hostname == "" {
		hostname = defaultHostname
	}
	r.mu.Unlock()

	connected := node != nil && node.IsConnected()
	url := ""
	authURL := ""
	if node != nil {
		url = node.URL()
		authURL = node.AuthURL()
	}

	return globalconfig.ConfigStatus{
		Config: struct {
			Enabled  bool   `json:"enabled"`
			Hostname string `json:"hostname"`
		}{
			Enabled:  enabled,
			Hostname: hostname,
		},
		Status: struct {
			Connected bool   `json:"connected"`
			URL       string `json:"url,omitempty"`
			AuthURL   string `json:"authUrl,omitempty"`
		}{
			Connected: connected,
			URL:       url,
			AuthURL:   authURL,
		},
	}
}

// publish sends the current status to the UI via the value channel.
func (r *Remote) publish() {
	if r.publisher == nil {
		return
	}
	r.publisher <- util.Param{Key: keys.Remote, Val: r.ConfigStatus()}
}
