//go:build windows

package sysproxy

import (
	"fmt"
	"log"

	"golang.org/x/sys/windows/registry"
)

const (
	internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

// Supported reports whether automatic system proxy configuration is available.
func Supported() bool { return true }

// Backup reads the current Windows proxy settings from the registry.
func Backup() (*ProxySettings, error) {
	settings, err := Current()
	if err != nil {
		return nil, err
	}

	log.Printf("[winproxy] Backed up: Enable=%d, Server=%q, Override=%q",
		settings.ProxyEnable, settings.ProxyServer, settings.ProxyOverride)
	return settings, nil
}

// Current reads the current Windows proxy settings from the registry.
func Current() (*ProxySettings, error) {
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
	} else if err != registry.ErrNotExist {
		return nil, fmt.Errorf("read ProxyEnable: %w", err)
	}

	// Read ProxyServer (string)
	s, _, err := key.GetStringValue("ProxyServer")
	if err == nil {
		settings.ProxyServer = s
		settings.HasServer = true
	} else if err != registry.ErrNotExist {
		return nil, fmt.Errorf("read ProxyServer: %w", err)
	}

	// Read ProxyOverride (string)
	s, _, err = key.GetStringValue("ProxyOverride")
	if err == nil {
		settings.ProxyOverride = s
		settings.HasOverride = true
	} else if err != registry.ErrNotExist {
		return nil, fmt.Errorf("read ProxyOverride: %w", err)
	}

	return settings, nil
}

// Enable sets the Windows system proxy to the specified HTTP proxy address.
func Enable(addr string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	// Write the address and bypass list first. ProxyEnable is deliberately last,
	// so a partial write does not redirect traffic to an incomplete setup.
	if err := key.SetStringValue("ProxyServer", addr); err != nil {
		return fmt.Errorf("failed to set ProxyServer: %w", err)
	}

	// Set ProxyOverride to bypass local addresses
	if err := key.SetStringValue("ProxyOverride", managedOverride); err != nil {
		return fmt.Errorf("failed to set ProxyOverride: %w", err)
	}
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("failed to set ProxyEnable: %w", err)
	}

	if err := notifyProxyChange(); err != nil {
		return fmt.Errorf("notify Windows of proxy change: %w", err)
	}

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

	// Disable the managed proxy before changing its address. If the original
	// configuration was enabled, it is enabled again only after all values are
	// restored.
	if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
		return fmt.Errorf("failed to disable proxy during restore: %w", err)
	}

	// Restore ProxyServer
	if backup.HasServer {
		if err := key.SetStringValue("ProxyServer", backup.ProxyServer); err != nil {
			return fmt.Errorf("failed to restore ProxyServer: %w", err)
		}
	} else if err := key.DeleteValue("ProxyServer"); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("failed to remove ProxyServer: %w", err)
	}

	// Restore ProxyOverride
	if backup.HasOverride {
		if err := key.SetStringValue("ProxyOverride", backup.ProxyOverride); err != nil {
			return fmt.Errorf("failed to restore ProxyOverride: %w", err)
		}
	} else if err := key.DeleteValue("ProxyOverride"); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("failed to remove ProxyOverride: %w", err)
	}
	if err := key.SetDWordValue("ProxyEnable", backup.ProxyEnable); err != nil {
		return fmt.Errorf("failed to restore ProxyEnable: %w", err)
	}

	if err := notifyProxyChange(); err != nil {
		return fmt.Errorf("notify Windows of restored proxy settings: %w", err)
	}

	log.Printf("[winproxy] Restored proxy settings: Enable=%d, Server=%q",
		backup.ProxyEnable, backup.ProxyServer)
	return nil
}
