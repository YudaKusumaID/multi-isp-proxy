//go:build linux

package netif

import "fmt"

// populateDetails on Linux doesn't need to do anything since net.Interfaces()
// already populates meaningful names like eth0, wlan0, etc.
func populateDetails(interfaces []*NetInterface) {
	// Dummy implementation for Linux
}

// GetFriendlyName returns a friendly display name for the interface.
func GetFriendlyName(ni *NetInterface) string {
	// On Linux, the interface Name (e.g., eth0) is usually sufficient.
	return fmt.Sprintf("%s (%s)", ni.Name, ni.IP.String())
}
