package balancer

import (
	"github.com/ayanacorp/venn-combine-connection/internal/netif"
)

// Mode represents the load balancing strategy.
type Mode int

const (
	ModeRoundRobin Mode = iota
	ModeFailover
)

// String returns the display name of the mode.
func (m Mode) String() string {
	switch m {
	case ModeRoundRobin:
		return "Round-Robin"
	case ModeFailover:
		return "Failover"
	default:
		return "Unknown"
	}
}

// Strategy is the interface for load balancing implementations.
type Strategy interface {
	// Next returns the next interface to use for a new connection.
	// Returns nil if no interface is available.
	Next() *netif.NetInterface

	// SetInterfaces updates the list of available interfaces.
	SetInterfaces(interfaces []*netif.NetInterface)

	// Mode returns the balancing mode.
	Mode() Mode
}

// New creates a new Strategy based on the given mode.
func New(mode Mode, interfaces []*netif.NetInterface) Strategy {
	switch mode {
	case ModeFailover:
		return NewFailover(interfaces)
	default:
		return NewRoundRobin(interfaces)
	}
}
