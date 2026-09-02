//go:build windows

package sysproxy

import (
	"fmt"
	"syscall"
)

var (
	wininet                = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOptionW = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// notifyProxyChange notifies the system that proxy settings have changed
// by calling InternetSetOption with INTERNET_OPTION_SETTINGS_CHANGED and INTERNET_OPTION_REFRESH.
func notifyProxyChange() error {
	for _, option := range []uintptr{internetOptionSettingsChanged, internetOptionRefresh} {
		result, _, callErr := procInternetSetOptionW.Call(0, option, 0, 0)
		if result == 0 {
			if callErr != syscall.Errno(0) {
				return callErr
			}
			return fmt.Errorf("InternetSetOptionW(%d) returned false", option)
		}
	}
	return nil
}
