package remote

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/evcc-io/evcc/util"
	"tailscale.com/tsnet"
)

// TsNode manages an embedded Tailscale node via tsnet.
type TsNode struct {
	mu            sync.Mutex
	srv           *tsnet.Server
	cancel        context.CancelFunc
	connected     bool
	tailnetURL    string
	authURL       string
	log           *util.Logger
	onStateChange func()
}

// NewTsNode creates a new TsNode.
func NewTsNode(log *util.Logger, onStateChange func()) *TsNode {
	return &TsNode{
		log:           log,
		onStateChange: onStateChange,
	}
}

// Start launches the embedded Tailscale node with the given settings.
// stateDir is the directory used to persist Tailscale state (keys, etc.).
func (n *TsNode) Start(stateDir, authKey, hostname string, httpHandler http.Handler) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.srv != nil {
		return nil // already running
	}

	if hostname == "" {
		hostname = defaultHostname
	}

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("tailscale state dir: %w", err)
	}

	srv := &tsnet.Server{
		Dir:      stateDir,
		Hostname: hostname,
		AuthKey:  authKey,
		Logf:     func(format string, args ...any) { n.log.TRACE.Printf(format, args...) },
	}

	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	n.srv = srv

	// Start in background; Up() waits until the node is fully online.
	go func() {
		status, err := srv.Up(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // cancelled by Close()
			}
			n.log.ERROR.Printf("tailscale up: %v", err)
			n.setState(false, "", "")
			return
		}

		// Determine the *.ts.net URL for this node.
		url := ""
		if status != nil && len(status.CertDomains) > 0 {
			url = "https://" + status.CertDomains[0]
		} else if status != nil && status.Self != nil && len(status.Self.DNSName) > 0 {
			url = "http://" + status.Self.DNSName
		}

		n.setState(true, url, "")
		n.log.INFO.Printf("tailscale connected: %s", url)

		ln, err := srv.Listen("tcp", ":80")
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			n.log.ERROR.Printf("tailscale listen: %v", err)
			n.setState(false, "", "")
			return
		}
		defer ln.Close()

		httpSrv := &http.Server{Handler: httpHandler}
		if err := httpSrv.Serve(ln); err != nil && ctx.Err() == nil {
			n.log.ERROR.Printf("tailscale http serve: %v", err)
		}

		n.setState(false, "", "")
	}()

	return nil
}

// Close shuts down the embedded Tailscale node.
func (n *TsNode) Close() {
	n.mu.Lock()
	srv := n.srv
	cancel := n.cancel
	n.srv = nil
	n.cancel = nil
	n.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		_ = srv.Close()
	}

	n.setState(false, "", "")
}

// IsConnected returns whether the Tailscale node is online.
func (n *TsNode) IsConnected() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.connected
}

// URL returns the *.ts.net URL for this node, or empty if not yet connected.
func (n *TsNode) URL() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.tailnetURL
}

// AuthURL returns the Tailscale authentication URL if interactive login is needed.
func (n *TsNode) AuthURL() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.authURL
}

// StateDir returns the recommended Tailscale state directory under the evcc home.
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".evcc", "tailscale")
	}
	return filepath.Join(home, ".evcc", "tailscale")
}

func (n *TsNode) setState(connected bool, url, authURL string) {
	n.mu.Lock()
	n.connected = connected
	n.tailnetURL = url
	n.authURL = authURL
	n.mu.Unlock()

	if n.onStateChange != nil {
		n.onStateChange()
	}
}
