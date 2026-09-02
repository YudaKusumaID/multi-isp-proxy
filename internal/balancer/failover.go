package balancer

import (
	"sync"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
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
	candidates := f.Candidates()
	for _, candidate := range candidates {
		if candidate.IsAlive() {
			return candidate
		}
	}
	return nil
}

// Candidates returns every live interface in priority order.
func (f *Failover) Candidates() []*netif.NetInterface {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.interfaces) == 0 {
		return nil
	}

	live := make([]*netif.NetInterface, 0, len(f.interfaces))
	down := make([]*netif.NetInterface, 0, len(f.interfaces))
	for _, iface := range f.interfaces {
		if iface.IsAlive() {
			live = append(live, iface)
		} else {
			down = append(down, iface)
		}
	}
	return append(live, down...)
}
