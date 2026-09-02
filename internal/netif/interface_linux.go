//go:build linux

package netif

// populateDetails on Linux doesn't need to do anything since net.Interfaces()
// already populates meaningful names like eth0, wlan0, etc.
func populateDetails(interfaces []*NetInterface) {
	// Dummy implementation for Linux
}
