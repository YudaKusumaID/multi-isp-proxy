package balancer

import (
	"sync"

	"github.com/ayanacorp/venn-combine-connection/internal/netif"
)

// Failover always uses the primary interface, switching to secondary only when primary is down.
// Automatically returns to primary when it recovers.
type Failover struct {
	interfaces []*netif.NetInterface
	mu         sync.Mutex
}

// NewFailover creates a new failover balancer.
func NewFailover(interfaces []*netif.NetInterface) *Failover {
	return &Failover{
		interfaces: interfaces,
	}
}

// Next returns the primary interface if alive, otherwise the first alive secondary.
func (f *Failover) Next() *netif.NetInterface {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.interfaces) == 0 {
		return nil
	}

	// Always prefer the primary (first) interface
	if f.interfaces[0].IsAlive() {
		return f.interfaces[0]
	}

	// Primary is down, try secondary interfaces in order
	for i := 1; i < len(f.interfaces); i++ {
		if f.interfaces[i].IsAlive() {
			return f.interfaces[i]
		}
	}

	// All down — return primary as fallback
	return f.interfaces[0]
}

// SetInterfaces updates the list of available interfaces.
func (f *Failover) SetInterfaces(interfaces []*netif.NetInterface) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interfaces = interfaces
}

// Mode returns ModeFailover.
func (f *Failover) Mode() Mode {
	return ModeFailover
}
