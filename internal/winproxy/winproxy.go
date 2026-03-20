package winproxy

import (
	"fmt"
	"log"

	"golang.org/x/sys/windows/registry"
)

const (
	internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

// ProxySettings holds the Windows proxy configuration for backup/restore.
type ProxySettings struct {
	ProxyEnable   uint32
	ProxyServer   string
	ProxyOverride string
	hasServer     bool
	hasOverride   bool
}

// Backup reads the current Windows proxy settings from the registry.
func Backup() (*ProxySettings, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	settings := &ProxySettings{}

	// Read ProxyEnable (DWORD)
	val, _, err := key.GetIntegerValue("ProxyEnable")
	if err == nil {
		settings.ProxyEnable = uint32(val)
	}

	// Read ProxyServer (string)
	s, _, err := key.GetStringValue("ProxyServer")
	if err == nil {
		settings.ProxyServer = s
		settings.hasServer = true
	}

	// Read ProxyOverride (string)
	s, _, err = key.GetStringValue("ProxyOverride")
	if err == nil {
		settings.ProxyOverride = s
		settings.hasOverride = true
	}

	log.Printf("[winproxy] Backed up: Enable=%d, Server=%q, Override=%q",
		settings.ProxyEnable, settings.ProxyServer, settings.ProxyOverride)

	return settings, nil
}

// Enable sets the Windows system proxy to the specified HTTP proxy address.
func Enable(addr string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	// Set ProxyEnable = 1
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("failed to set ProxyEnable: %w", err)
	}

	// Set ProxyServer = "<addr>" (HTTP proxy format — universally compatible)
	if err := key.SetStringValue("ProxyServer", addr); err != nil {
		return fmt.Errorf("failed to set ProxyServer: %w", err)
	}

	// Set ProxyOverride to bypass local addresses
	override := "localhost;127.0.0.1;10.*;192.168.*;<local>"
	if err := key.SetStringValue("ProxyOverride", override); err != nil {
		return fmt.Errorf("failed to set ProxyOverride: %w", err)
	}

	// Notify the system that proxy settings have changed
	notifyProxyChange()

	log.Printf("[winproxy] Enabled proxy: %s", addr)
	return nil
}

// Restore reverts Windows proxy settings to the backed-up values.
func Restore(backup *ProxySettings) error {
	if backup == nil {
		return fmt.Errorf("no backup to restore")
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	// Restore ProxyEnable
	if err := key.SetDWordValue("ProxyEnable", backup.ProxyEnable); err != nil {
		return fmt.Errorf("failed to restore ProxyEnable: %w", err)
	}

	// Restore ProxyServer
	if backup.hasServer {
		if err := key.SetStringValue("ProxyServer", backup.ProxyServer); err != nil {
			return fmt.Errorf("failed to restore ProxyServer: %w", err)
		}
	}

	// Restore ProxyOverride
	if backup.hasOverride {
		if err := key.SetStringValue("ProxyOverride", backup.ProxyOverride); err != nil {
			return fmt.Errorf("failed to restore ProxyOverride: %w", err)
		}
	}

	// Notify the system that proxy settings have changed
	notifyProxyChange()

	log.Printf("[winproxy] Restored proxy settings: Enable=%d, Server=%q",
		backup.ProxyEnable, backup.ProxyServer)
	return nil
}

// Disable turns off the Windows system proxy without restoring previous settings.
func Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
		return fmt.Errorf("failed to disable proxy: %w", err)
	}

	notifyProxyChange()

	log.Printf("[winproxy] Proxy disabled")
	return nil
}
