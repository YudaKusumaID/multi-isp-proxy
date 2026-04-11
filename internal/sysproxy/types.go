package sysproxy

// ProxySettings holds the Windows proxy configuration for backup/restore.
type ProxySettings struct {
	ProxyEnable   uint32
	ProxyServer   string
	ProxyOverride string
	hasServer     bool
	hasOverride   bool
}
