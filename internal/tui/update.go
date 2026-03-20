package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ayanacorp/venn-combine-connection/internal/balancer"
	"github.com/ayanacorp/venn-combine-connection/internal/netif"
	"github.com/ayanacorp/venn-combine-connection/internal/proxy"
	"github.com/ayanacorp/venn-combine-connection/internal/winproxy"
)

// Update handles all messages and user input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tickMsg:
		if m.phase == PhaseRunning {
			return m, doTick()
		}
		return m, nil

	case proxyStartedMsg:
		m.phase = PhaseRunning
		m.message = "Proxy is running!"
		return m, doTick()

	case proxyErrorMsg:
		m.err = msg.err
		m.phase = PhaseStopping
		return m, nil
	}

	return m, nil
}

// handleKey processes keyboard input based on the current phase.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit
	if key == "ctrl+c" {
		return m.shutdown()
	}

	switch m.phase {

	case PhaseSelectInterfaces:
		return m.handleSelectInterfaces(key)

	case PhaseSelectMode:
		return m.handleSelectMode(key)

	case PhaseConfirmWinProxy:
		return m.handleConfirmWinProxy(key)

	case PhaseRunning:
		return m.handleRunning(key)
	}

	return m, nil
}

// handleSelectInterfaces handles interface selection.
func (m Model) handleSelectInterfaces(key string) (tea.Model, tea.Cmd) {
	if m.err != nil {
		if key == "q" || key == "enter" {
			return m, tea.Quit
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.allIfaces)-1 {
			m.cursor++
		}
	case "space":
		m.selected[m.cursor] = !m.selected[m.cursor]
		if !m.selected[m.cursor] {
			delete(m.selected, m.cursor)
		}
	case "a":
		// Toggle select all
		if m.countSelected() == len(m.allIfaces) {
			m.selected = make(map[int]bool)
		} else {
			for i := range m.allIfaces {
				m.selected[i] = true
			}
		}
	case "enter":
		if m.countSelected() >= 2 {
			m.phase = PhaseSelectMode
			m.cursor = 0
			m.message = ""
		} else if m.countSelected() == 1 {
			// Single interface → failover mode
			m.mode = balancer.ModeFailover
			m.phase = PhaseConfirmWinProxy
			m.cursor = 0
			m.message = ""
		} else {
			m.message = "Select at least 1 interface"
		}
	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// handleSelectMode handles mode selection.
func (m Model) handleSelectMode(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j", "tab":
		if m.mode == balancer.ModeRoundRobin {
			m.mode = balancer.ModeFailover
		} else {
			m.mode = balancer.ModeRoundRobin
		}
	case "enter":
		m.phase = PhaseConfirmWinProxy
		m.cursor = 0
	case "backspace", "escape":
		m.phase = PhaseSelectInterfaces
	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// handleConfirmWinProxy handles the Windows proxy auto-setup confirmation.
func (m Model) handleConfirmWinProxy(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		m.winProxyAuto = true
		return m.startProxy()
	case "n", "N":
		m.winProxyAuto = false
		return m.startProxy()
	case "backspace", "escape":
		if m.countSelected() >= 2 {
			m.phase = PhaseSelectMode
		} else {
			m.phase = PhaseSelectInterfaces
		}
	case "q":
		return m, tea.Quit
	}

	return m, nil
}

// handleRunning handles keys during the running phase.
func (m Model) handleRunning(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m.shutdown()
	case "r":
		for _, iface := range m.getSelectedInterfaces() {
			iface.ResetStats()
		}
		m.message = "Stats reset"
	}

	return m, nil
}

// startProxy initializes and starts the proxy with selected interfaces.
func (m Model) startProxy() (tea.Model, tea.Cmd) {
	selected := m.getSelectedInterfaces()

	// Create load balancer
	m.bal = balancer.New(m.mode, selected)

	// Create proxy server
	m.proxyServer = proxy.NewServer(m.proxyAddr, m.bal)

	// Setup Windows proxy if requested
	if m.winProxyAuto {
		backup, err := winproxy.Backup()
		if err != nil {
			m.err = err
			return m, nil
		}
		m.proxyBackup = backup

		if err := winproxy.Enable(m.proxyAddr); err != nil {
			m.err = err
			return m, nil
		}
	}

	// Start health monitor
	go netif.Monitor(context.Background(), selected, 5*time.Second)

	// Start proxy in background
	cmd := func() tea.Msg {
		err := m.proxyServer.Start(context.Background())
		if err != nil {
			return proxyErrorMsg{err: err}
		}
		return nil
	}

	m.phase = PhaseRunning
	m.message = "Proxy started on " + m.proxyAddr

	return m, tea.Batch(tea.Cmd(cmd), doTick())
}

// shutdown stops the proxy and restores Windows settings.
func (m Model) shutdown() (tea.Model, tea.Cmd) {
	m.phase = PhaseStopping

	// Stop proxy
	if m.proxyServer != nil {
		m.proxyServer.Stop()
	}

	// Restore Windows proxy settings
	if m.winProxyAuto && m.proxyBackup != nil {
		winproxy.Restore(m.proxyBackup)
	}

	return m, tea.Quit
}

// getSelectedInterfaces returns the selected interfaces.
func (m Model) getSelectedInterfaces() []*netif.NetInterface {
	var result []*netif.NetInterface
	for i, iface := range m.allIfaces {
		if m.selected[i] {
			result = append(result, iface)
		}
	}
	return result
}

// countSelected returns the number of selected interfaces.
func (m Model) countSelected() int {
	count := 0
	for _, v := range m.selected {
		if v {
			count++
		}
	}
	return count
}
