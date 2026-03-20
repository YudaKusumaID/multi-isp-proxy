package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

var (
	// Colors
	colorPrimary   = lg.Color("#7C3AED")
	colorSecondary = lg.Color("#06B6D4")
	colorSuccess   = lg.Color("#10B981")
	colorDanger    = lg.Color("#EF4444")
	colorWarning   = lg.Color("#F59E0B")
	colorMuted     = lg.Color("#6B7280")
	colorBg        = lg.Color("#1F2937")
	colorFg        = lg.Color("#F9FAFB")
	colorBorder    = lg.Color("#374151")

	// Styles
	titleStyle = lg.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			PaddingBottom(1)

	subtitleStyle = lg.NewStyle().
			Foreground(colorSecondary).
			Bold(true)

	selectedStyle = lg.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	cursorStyle = lg.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	mutedStyle = lg.NewStyle().
			Foreground(colorMuted)

	errorStyle = lg.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	statusUpStyle = lg.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	statusDownStyle = lg.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	boxStyle = lg.NewStyle().
			Border(lg.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	headerStyle = lg.NewStyle().
			Bold(true).
			Foreground(colorFg).
			Background(colorPrimary).
			Padding(0, 2).
			MarginBottom(1)

	helpStyle = lg.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)
)

// View renders the TUI based on the current phase.
func (m Model) View() tea.View {
	var b strings.Builder

	// Header
	b.WriteString(headerStyle.Render("⚡ Venn Combine Connection"))
	b.WriteString("\n\n")

	switch m.phase {
	case PhaseSelectInterfaces:
		b.WriteString(m.viewSelectInterfaces())
	case PhaseSelectMode:
		b.WriteString(m.viewSelectMode())
	case PhaseConfirmWinProxy:
		b.WriteString(m.viewConfirmWinProxy())
	case PhaseRunning:
		b.WriteString(m.viewRunning())
	case PhaseStopping:
		b.WriteString(m.viewStopping())
	}

	return tea.NewView(b.String())
}

// viewSelectInterfaces renders the interface selection screen.
func (m Model) viewSelectInterfaces() string {
	var b strings.Builder

	if m.err != nil {
		b.WriteString(errorStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Press q to exit"))
		return boxStyle.Render(b.String())
	}

	b.WriteString(titleStyle.Render("Select Network Interfaces"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Use ↑↓ to navigate, Space to select, Enter to confirm"))
	b.WriteString("\n\n")

	for i, iface := range m.allIfaces {
		// Cursor
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("▸ ")
		}

		// Checkbox
		check := "○"
		if m.selected[i] {
			check = selectedStyle.Render("●")
		}

		// Interface info
		name := iface.FriendlyName
		if name == "" {
			name = iface.Name
		}
		ip := iface.IP.String()

		// Status
		status := statusUpStyle.Render("UP")
		if !iface.IsAlive() {
			status = statusDownStyle.Render("DOWN")
		}

		// Gateway info
		gw := ""
		if iface.Gateway != "" {
			gw = mutedStyle.Render(fmt.Sprintf(" gw:%s", iface.Gateway))
		}

		line := fmt.Sprintf("%s%s %s  %s  [%s]%s", cursor, check, name, mutedStyle.Render(ip), status, gw)
		b.WriteString(line)
		b.WriteString("\n")
	}

	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(m.message))
	}

	selected := m.countSelected()
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("Selected: %d interface(s)", selected)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Space: toggle • a: select all • Enter: confirm • q: quit"))

	return boxStyle.Render(b.String())
}

// viewSelectMode renders the mode selection screen.
func (m Model) viewSelectMode() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Select Balancing Mode"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Use ↑↓ or Tab to switch, Enter to confirm"))
	b.WriteString("\n\n")

	// Round-Robin
	rrCursor := "  "
	rrStyle := mutedStyle
	if m.mode == 0 {
		rrCursor = cursorStyle.Render("▸ ")
		rrStyle = selectedStyle
	}
	b.WriteString(fmt.Sprintf("%s%s\n", rrCursor, rrStyle.Render("Round-Robin")))
	b.WriteString(mutedStyle.Render("    Distribute connections across all interfaces evenly."))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("    Best for: download managers, torrent, multi-tab browsing"))
	b.WriteString("\n\n")

	// Failover
	foCursor := "  "
	foStyle := mutedStyle
	if m.mode == 1 {
		foCursor = cursorStyle.Render("▸ ")
		foStyle = selectedStyle
	}
	b.WriteString(fmt.Sprintf("%s%s\n", foCursor, foStyle.Render("Failover")))
	b.WriteString(mutedStyle.Render("    Use primary interface, auto-switch when down."))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("    Best for: single-connection apps, streaming"))
	b.WriteString("\n\n")

	b.WriteString(helpStyle.Render("↑↓/Tab: switch • Enter: confirm • Esc: back • q: quit"))

	return boxStyle.Render(b.String())
}

// viewConfirmWinProxy renders the Windows proxy auto-setup confirmation.
func (m Model) viewConfirmWinProxy() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Windows Proxy Setup"))
	b.WriteString("\n\n")

	b.WriteString("Auto-configure Windows to use this proxy?\n\n")
	b.WriteString(subtitleStyle.Render("What this does:"))
	b.WriteString("\n")
	b.WriteString("  • Sets system SOCKS proxy to " + selectedStyle.Render(m.proxyAddr) + "\n")
	b.WriteString("  • Bypasses localhost and local networks\n")
	b.WriteString("  • " + selectedStyle.Render("Automatically restores") + " original settings on exit\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render("Error: "+m.err.Error()) + "\n\n")
	}

	b.WriteString(subtitleStyle.Render("[Y]") + " Yes, auto-setup    ")
	b.WriteString(subtitleStyle.Render("[N]") + " No, I'll configure manually\n\n")

	b.WriteString(helpStyle.Render("Y/N: choose • Esc: back • q: quit"))

	return boxStyle.Render(b.String())
}

// viewRunning renders the running proxy status display.
func (m Model) viewRunning() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Proxy Running"))
	b.WriteString("\n\n")

	// Proxy info
	b.WriteString(subtitleStyle.Render("HTTP Proxy: "))
	b.WriteString(selectedStyle.Render(m.proxyAddr))
	if m.proxyServer != nil && m.proxyServer.Socks5Addr() != "" {
		b.WriteString("  ")
		b.WriteString(subtitleStyle.Render("SOCKS5: "))
		b.WriteString(selectedStyle.Render(m.proxyServer.Socks5Addr()))
	}
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Mode: "))
	b.WriteString(selectedStyle.Render(m.mode.String()))
	if m.winProxyAuto {
		b.WriteString("  ")
		b.WriteString(subtitleStyle.Render("WinProxy: "))
		b.WriteString(selectedStyle.Render("ON"))
	}
	b.WriteString("\n\n")

	// Interface stats
	b.WriteString(subtitleStyle.Render("Interface Status"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("─────────────────────────────────────────────────"))
	b.WriteString("\n")

	selected := m.getSelectedInterfaces()
	for _, iface := range selected {
		// Status indicator
		status := statusUpStyle.Render("●")
		if !iface.IsAlive() {
			status = statusDownStyle.Render("●")
		}

		name := iface.FriendlyName
		if name == "" {
			name = iface.Name
		}

		sent, recv := iface.Stats()
		b.WriteString(fmt.Sprintf("  %s %-20s %s  ↑ %s  ↓ %s\n",
			status,
			name,
			mutedStyle.Render(iface.IP.String()),
			formatBytes(sent),
			formatBytes(recv),
		))
	}

	// Proxy stats
	b.WriteString("\n")
	if m.proxyServer != nil {
		stats := m.proxyServer.GetStats()
		b.WriteString(subtitleStyle.Render("Connections"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("─────────────────────────────────────────────────"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  Active: %s  Total: %s\n",
			selectedStyle.Render(fmt.Sprintf("%d", stats.ActiveConnections)),
			mutedStyle.Render(fmt.Sprintf("%d", stats.TotalConnections)),
		))
	}

	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(selectedStyle.Render("ℹ " + m.message))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("r: reset stats • q: quit & restore"))

	return boxStyle.Render(b.String())
}

// viewStopping renders the stopping screen.
func (m Model) viewStopping() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Shutting Down..."))
	b.WriteString("\n\n")

	if m.winProxyAuto {
		b.WriteString(selectedStyle.Render("✓") + " Windows proxy settings restored\n")
	}
	b.WriteString(selectedStyle.Render("✓") + " Proxy server stopped\n")

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("Error: " + m.err.Error()))
	}

	return boxStyle.Render(b.String())
}

// formatBytes formats bytes into human-readable format.
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
