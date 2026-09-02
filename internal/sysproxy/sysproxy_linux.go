//go:build linux

package sysproxy

import "log"

// Supported reports whether automatic system proxy configuration is available.
func Supported() bool { return false }

// Backup returns a dummy proxy settings struct on Linux.
func Backup() (*ProxySettings, error) {
	log.Printf("[sysproxy] Backup proxy settings: Auto-proxy not supported natively on Linux.")
	return &ProxySettings{}, nil
}

// Current returns an empty configuration because Linux setup is manual.
func Current() (*ProxySettings, error) { return &ProxySettings{}, nil }

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
