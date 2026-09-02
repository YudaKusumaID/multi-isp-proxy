//go:build windows

package netif

import (
	"os/exec"
	"strings"
)

// populateDetails fills in friendly names and gateway info using PowerShell on Windows.
func populateDetails(interfaces []*NetInterface) {
	// Query Windows for adapter details via PowerShell
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-NetIPConfiguration | Where-Object { $_.IPv4Address -ne $null } | ForEach-Object {
			$alias = $_.InterfaceAlias
			$ip = ($_.IPv4Address | Select-Object -First 1).IPAddress
			$gw = ($_.IPv4DefaultGateway | Select-Object -First 1).NextHop
			"$alias||$ip||$gw"
		}`)

	out, err := cmd.Output()
	if err != nil {
		return
	}

	// Parse output: "InterfaceAlias||IPAddress||Gateway"
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	lookup := make(map[string]struct {
		alias   string
		gateway string
	})

	for _, line := range lines {
		line = strings.TrimSpace(line)
		parts := strings.Split(line, "||")
		if len(parts) < 3 {
			continue
		}
		alias := strings.TrimSpace(parts[0])
		ip := strings.TrimSpace(parts[1])
		gw := strings.TrimSpace(parts[2])
		lookup[ip] = struct {
			alias   string
			gateway string
		}{alias: alias, gateway: gw}
	}

	for _, ni := range interfaces {
		if info, ok := lookup[ni.IP.String()]; ok {
			ni.FriendlyName = info.alias
			ni.Gateway = info.gateway
		}
	}
}
