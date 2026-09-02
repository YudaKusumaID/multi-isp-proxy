package balancer

import (
	"sync"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
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
	candidates := r.Candidates()
	for _, candidate := range candidates {
		if candidate.IsAlive() {
			return candidate
		}
	}
	return nil
}

// Candidates returns every live interface, beginning at the current
// round-robin position. The position advances once per logical connection.
func (r *RoundRobin) Candidates() []*netif.NetInterface {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(r.interfaces)
	if n == 0 {
		return nil
	}

	live := make([]*netif.NetInterface, 0, n)
	down := make([]*netif.NetInterface, 0, n)
	firstIndex := -1
	for i := 0; i < n; i++ {
		idx := (r.index + i) % n
		iface := r.interfaces[idx]
		if iface.IsAlive() {
			if firstIndex == -1 {
				firstIndex = idx
			}
			live = append(live, iface)
		} else {
			down = append(down, iface)
		}
	}

	if firstIndex != -1 {
		r.index = (firstIndex + 1) % n
	} else {
		r.index = (r.index + 1) % n
	}
	return append(live, down...)
}
