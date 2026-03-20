package balancer

import (
	"sync"

	"github.com/ayanacorp/venn-combine-connection/internal/netif"
)

// RoundRobin distributes connections across interfaces in a cyclic manner.
// Skips interfaces that are currently down.
type RoundRobin struct {
	interfaces []*netif.NetInterface
	index      int
	mu         sync.Mutex
}

// NewRoundRobin creates a new round-robin balancer.
func NewRoundRobin(interfaces []*netif.NetInterface) *RoundRobin {
	return &RoundRobin{
		interfaces: interfaces,
		index:      0,
	}
}

// Next returns the next available interface in round-robin order.
func (r *RoundRobin) Next() *netif.NetInterface {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(r.interfaces)
	if n == 0 {
		return nil
	}

	// Try all interfaces starting from the current index
	for i := 0; i < n; i++ {
		idx := (r.index + i) % n
		iface := r.interfaces[idx]
		if iface.IsAlive() {
			r.index = (idx + 1) % n
			return iface
		}
	}

	// All interfaces are down — return first one anyway as a fallback
	r.index = 1 % n
	return r.interfaces[0]
}

// SetInterfaces updates the list of available interfaces.
func (r *RoundRobin) SetInterfaces(interfaces []*netif.NetInterface) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interfaces = interfaces
	r.index = 0
}

// Mode returns ModeRoundRobin.
func (r *RoundRobin) Mode() Mode {
	return ModeRoundRobin
}
