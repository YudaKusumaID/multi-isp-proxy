package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ayanacorp/venn-combine-connection/internal/balancer"
	"github.com/ayanacorp/venn-combine-connection/internal/netif"
	"github.com/ayanacorp/venn-combine-connection/internal/proxy"
	"github.com/ayanacorp/venn-combine-connection/internal/sysproxy"
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

	// Running phase
	bal         balancer.Strategy
	proxyServer *proxy.Server
	proxyBackup *sysproxy.ProxySettings

	// Stats
	statsTimer time.Time

	// Errors and messages
	err     error
	message string

	// Dimensions
	width  int
	height int
}

// NewModel creates the initial model with discovered interfaces.
func NewModel(proxyAddr string) Model {
	ifaces, err := netif.Discover()

	m := Model{
		phase:     PhaseSelectInterfaces,
		allIfaces: ifaces,
		selected:  make(map[int]bool),
		proxyAddr: proxyAddr,
		mode:      balancer.ModeRoundRobin,
		err:       err,
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

// proxyStartedMsg indicates the proxy has started.
type proxyStartedMsg struct{}

// proxyErrorMsg indicates a proxy error.
type proxyErrorMsg struct {
	err error
}
