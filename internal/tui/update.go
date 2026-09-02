package tui

import (
	"context"
	"errors"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/app"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/balancer"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/sysproxy"
)

// Update handles all messages and user input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tickMsg:
		if m.phase == PhaseRunning {
			return m, doTick()
		}
		return m, nil

	case proxyErrorMsg:
		m.err = msg.err
		m.phase = PhaseStopping
		if cleanupErr := m.Cleanup(); cleanupErr != nil {
			m.err = errors.Join(m.err, cleanupErr)
		}
		return m, tea.Quit
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
	if m.cleanupPending {
		if key == "q" {
			return m.shutdown()
		}
		return m, nil
	}

	switch key {
	case "y", "Y":
		m.winProxyAuto = runtime.GOOS == "windows"
		return m.startProxy()
	case "n", "N":
		if runtime.GOOS != "windows" {
			if m.countSelected() >= 2 {
				m.phase = PhaseSelectMode
			} else {
				m.phase = PhaseSelectInterfaces
			}
			return m, nil
		}
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
	m.err = nil
	selected := m.getSelectedInterfaces()

	m.winProxyAuto = m.winProxyAuto && sysproxy.Supported()
	var systemProxy app.SystemProxy
	if m.sysProxy != nil {
		systemProxy = m.sysProxy
	}
	m.session = app.NewSession(app.Config{
		Proxy:           m.proxyConfig,
		Mode:            m.mode,
		Interfaces:      selected,
		AutoSystemProxy: m.winProxyAuto,
		SystemProxy:     systemProxy,
		HealthInterval:  5 * time.Second,
	})

	m.lifecycle.mu.Lock()
	m.lifecycle.cleaned = false
	m.lifecycle.session = m.session
	m.lifecycle.mu.Unlock()

	if err := m.session.Start(context.Background()); err != nil {
		m.err = err
		if cleanupErr := m.Cleanup(); cleanupErr != nil {
			m.err = errors.Join(m.err, cleanupErr)
			m.cleanupPending = true
		}
		return m, nil
	}
	m.cleanupPending = false
	m.proxyAddr = m.session.HTTPAddr()

	cmd := func() tea.Msg {
		err := m.session.Wait()
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

	if err := m.Cleanup(); err != nil {
		m.err = errors.Join(m.err, err)
	}

	return m, tea.Quit
}

// Cleanup stops the proxy and restores system settings once. Bubble Tea copies
// Model values, so lifecycleState is shared by pointer between all copies.
func (m Model) Cleanup() error {
	if m.lifecycle == nil {
		return nil
	}

	m.lifecycle.mu.Lock()
	defer m.lifecycle.mu.Unlock()

	if m.lifecycle.cleaned {
		return nil
	}

	if m.lifecycle.session != nil {
		if err := m.lifecycle.session.Stop(); err != nil {
			return err
		}
	}

	m.lifecycle.cleaned = true
	m.lifecycle.session = nil
	return nil
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
