package netif

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// NetInterface represents a network interface with its connection details.
type NetInterface struct {
	Name      string
	FriendlyName string
	IP        net.IP
	Gateway   string
	Alive     bool
	BytesSent uint64
	BytesRecv uint64
	mu        sync.RWMutex
}

// IsAlive checks if the interface can reach the internet by dialing a DNS server.
func (n *NetInterface) IsAlive() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Alive
}

// SetAlive updates the alive status.
func (n *NetInterface) SetAlive(alive bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Alive = alive
}

// AddBytesSent atomically adds to the sent counter.
func (n *NetInterface) AddBytesSent(b uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.BytesSent += b
}

// AddBytesRecv atomically adds to the received counter.
func (n *NetInterface) AddBytesRecv(b uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.BytesRecv += b
}

// Stats returns the current byte counters.
func (n *NetInterface) Stats() (sent, recv uint64) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.BytesSent, n.BytesRecv
}

// ResetStats resets the byte counters to zero.
func (n *NetInterface) ResetStats() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.BytesSent = 0
	n.BytesRecv = 0
}

// String returns a readable representation.
func (n *NetInterface) String() string {
	status := "UP"
	if !n.IsAlive() {
		status = "DOWN"
	}
	name := n.FriendlyName
	if name == "" {
		name = n.Name
	}
	return fmt.Sprintf("%s (%s) [%s]", name, n.IP.String(), status)
}

// Discover enumerates all active network interfaces with valid IPv4 addresses.
func Discover() ([]*NetInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}

	var result []*NetInterface
	for _, iface := range ifaces {
		// Skip loopback, down, and non-active interfaces
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Only IPv4, skip loopback and link-local
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.To4() == nil {
				continue
			}

			ni := &NetInterface{
				Name:         iface.Name,
				FriendlyName: iface.Name,
				IP:           ip.To4(),
				Alive:        true,
			}
			result = append(result, ni)
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no active network interfaces found")
	}

	// Populate friendly names and gateways (platform-specific)
	populateDetails(result)

	return result, nil
}

// CheckHealth tests if a network interface can reach the internet
// by binding to its IP and connecting to a DNS server.
func CheckHealth(ni *NetInterface) bool {
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		LocalAddr: &net.TCPAddr{IP: ni.IP},
	}
	// Try primary check (Cloudflare)
	conn, err := dialer.Dial("tcp", "1.1.1.1:443")
	if err == nil {
		conn.Close()
		return true
	}

	// If Cloudflare fails, try fallback (Google DNS)
	conn, err = dialer.Dial("tcp", "8.8.8.8:53")
	if err == nil {
		conn.Close()
		return true
	}

	return false
}

// Monitor periodically checks the health of all interfaces and updates their status.
func Monitor(ctx context.Context, interfaces []*NetInterface, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, ni := range interfaces {
				alive := CheckHealth(ni)
				ni.SetAlive(alive)
			}
		}
	}
}
