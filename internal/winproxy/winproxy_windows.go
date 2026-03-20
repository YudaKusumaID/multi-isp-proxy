package winproxy

import (
	"syscall"
	"unsafe"
)

var (
	wininet                    = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOptionW     = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// notifyProxyChange notifies the system that proxy settings have changed
// by calling InternetSetOption with INTERNET_OPTION_SETTINGS_CHANGED and INTERNET_OPTION_REFRESH.
func notifyProxyChange() {
	procInternetSetOptionW.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOptionW.Call(0, internetOptionRefresh, 0, 0)
	_ = unsafe.Sizeof(0) // prevent unused import
}
