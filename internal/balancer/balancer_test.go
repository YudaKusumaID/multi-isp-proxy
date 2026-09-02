package balancer

import (
	"net"
	"testing"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
)

func testInterface(ip string, alive bool) *netif.NetInterface {
	iface := &netif.NetInterface{IP: net.ParseIP(ip)}
	iface.SetAlive(alive)
	return iface
}

func TestRoundRobinCyclesAndSkipsUnavailableInterfaces(t *testing.T) {
	first := testInterface("192.0.2.1", true)
	second := testInterface("192.0.2.2", false)
	third := testInterface("192.0.2.3", true)
	strategy := NewRoundRobin([]*netif.NetInterface{first, second, third})

	want := []*netif.NetInterface{first, third, first, third}
	for index, expected := range want {
		if actual := strategy.Next(); actual != expected {
			t.Fatalf("selection %d: got %p, want %p", index, actual, expected)
		}
	}

	first.SetAlive(false)
	third.SetAlive(false)
	if actual := strategy.Next(); actual != nil {
		t.Fatalf("all interfaces unavailable: got %v, want nil", actual)
	}
}

func TestFailoverPrefersPrimaryAndUsesFirstAvailableSecondary(t *testing.T) {
	primary := testInterface("192.0.2.1", true)
	secondary := testInterface("192.0.2.2", true)
	strategy := NewFailover([]*netif.NetInterface{primary, secondary})

	if actual := strategy.Next(); actual != primary {
		t.Fatalf("healthy primary: got %p, want %p", actual, primary)
	}

	primary.SetAlive(false)
	if actual := strategy.Next(); actual != secondary {
		t.Fatalf("failed primary: got %p, want %p", actual, secondary)
	}
}

func TestCandidatesKeepDownInterfacesAsLastResort(t *testing.T) {
	primary := testInterface("192.0.2.1", false)
	secondary := testInterface("192.0.2.2", true)
	failover := NewFailover([]*netif.NetInterface{primary, secondary})
	candidates := failover.Candidates()
	if len(candidates) != 2 || candidates[0] != secondary || candidates[1] != primary {
		t.Fatalf("failover candidates = %v, want live secondary then down primary", candidates)
	}

	secondary.SetAlive(false)
	candidates = failover.Candidates()
	if len(candidates) != 2 {
		t.Fatalf("all-down candidates = %v, want both as fallback", candidates)
	}
	if failover.Next() != nil {
		t.Fatal("Next should remain nil when health marks every interface down")
	}
}
