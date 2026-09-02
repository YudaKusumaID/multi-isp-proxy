package tui

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/app"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/balancer"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/proxy"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/sysproxy"
)

// Phase represents the current phase of the TUI.
type Phase int

const (
	PhaseSelectInterfaces Phase = iota
	PhaseSelectMode
	PhaseConfirmWinProxy
	PhaseRunning
	PhaseStopping
)

// Model is the Bubbletea model holding all application state.
type Model struct {
	// Setup phase
	phase     Phase
	allIfaces []*netif.NetInterface
	cursor    int
	selected  map[int]bool

	// Configuration
	mode         balancer.Mode
	winProxyAuto bool
	proxyAddr    string
	proxyConfig  proxy.Config
	sysProxy     *sysproxy.Manager

	// Running phase
	session *app.Session

	// Errors and messages
	err            error
	message        string
	cleanupPending bool

	// Shared by all value copies of Model created by Bubble Tea.
	lifecycle *lifecycleState
}

type lifecycleState struct {
	mu      sync.Mutex
	cleaned bool
	session *app.Session
}

// Config contains runtime dependencies and proxy security settings.
type Config struct {
	Proxy       proxy.Config
	SystemProxy *sysproxy.Manager
}

// NewModel creates the initial model with discovered interfaces.
func NewModel(proxyAddr string) Model {
	return NewModelWithConfig(Config{Proxy: proxy.Config{HTTPAddr: proxyAddr}})
}

// NewModelWithConfig creates a model with explicit runtime configuration.
func NewModelWithConfig(config Config) Model {
	ifaces, err := netif.Discover()

	m := Model{
		phase:       PhaseSelectInterfaces,
		allIfaces:   ifaces,
		selected:    make(map[int]bool),
		proxyAddr:   config.Proxy.HTTPAddr,
		proxyConfig: config.Proxy,
		sysProxy:    config.SystemProxy,
		mode:        balancer.ModeRoundRobin,
		err:         err,
		lifecycle:   &lifecycleState{},
	}

	return m
}

// Init is the Bubbletea init function.
func (m Model) Init() tea.Cmd {
	return nil
}

// tickMsg is sent periodically to update stats.
type tickMsg time.Time

// doTick creates a tick command for stats refresh.
func doTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// proxyErrorMsg indicates a proxy error.
type proxyErrorMsg struct {
	err error
}
