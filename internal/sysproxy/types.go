package sysproxy

const managedOverride = "localhost;127.0.0.1;10.*;192.168.*;<local>"

// ProxySettings holds the Windows proxy configuration for backup/restore.
type ProxySettings struct {
	ProxyEnable   uint32
	ProxyServer   string
	ProxyOverride string
	HasServer     bool
	HasOverride   bool
}
