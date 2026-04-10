//go:build linux

package sysproxy

import "log"

// Backup returns a dummy proxy settings struct on Linux.
func Backup() (*ProxySettings, error) {
	log.Printf("[sysproxy] Backup proxy settings: Auto-proxy not supported natively on Linux.")
	return &ProxySettings{}, nil
}

// Enable simply logs that manual configuration is required on Linux.
func Enable(addr string) error {
	log.Printf("[sysproxy] Linux detected. Please manually configure your browser/apps to use proxy: %s", addr)
	return nil
}

// Restore simply logs that restore is not needed on Linux.
func Restore(backup *ProxySettings) error {
	log.Printf("[sysproxy] Restore proxy settings: No action taken on Linux.")
	return nil
}

// Disable simply logs that disable is not needed on Linux.
func Disable() error {
	log.Printf("[sysproxy] Disable proxy: No action taken on Linux.")
	return nil
}
