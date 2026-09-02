package app

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/balancer"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/proxy"
)

type fakeSystemProxy struct {
	active     bool
	beginErr   error
	restoreErr error
	beginAddr  string
	ready      bool
	restores   int
}

func (f *fakeSystemProxy) Begin(address string) error {
	f.beginAddr = address
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err == nil {
		f.ready = true
		_ = conn.Close()
	}
	f.active = true
	return f.beginErr
}

func (f *fakeSystemProxy) Restore() error {
	f.restores++
	if f.restoreErr != nil {
		return f.restoreErr
	}
	f.active = false
	return nil
}

func (f *fakeSystemProxy) Active() bool { return f.active }

func TestSessionMakesListenerReadyBeforeSystemProxy(t *testing.T) {
	systemProxy := &fakeSystemProxy{}
	session := NewSession(Config{
		Proxy:           proxy.Config{HTTPAddr: "127.0.0.1:0"},
		Mode:            balancer.ModeFailover,
		Interfaces:      []*netif.NetInterface{loopbackInterface()},
		AutoSystemProxy: true,
		SystemProxy:     systemProxy,
		HealthInterval:  time.Hour,
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !systemProxy.ready || systemProxy.beginAddr == "127.0.0.1:0" {
		t.Fatalf("system proxy enabled before listener readiness: %+v", systemProxy)
	}
	if err := session.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if systemProxy.active || systemProxy.restores != 1 {
		t.Fatalf("system proxy not restored exactly once: %+v", systemProxy)
	}
	if err := session.Stop(); err != nil || systemProxy.restores != 1 {
		t.Fatalf("second Stop was not idempotent: err=%v restores=%d", err, systemProxy.restores)
	}
}

func TestSessionKeepsFailedRollbackRetryable(t *testing.T) {
	systemProxy := &fakeSystemProxy{
		beginErr:   errors.New("enable failed"),
		restoreErr: errors.New("rollback failed"),
	}
	session := NewSession(Config{
		Proxy:           proxy.Config{HTTPAddr: "127.0.0.1:0"},
		Mode:            balancer.ModeFailover,
		Interfaces:      []*netif.NetInterface{loopbackInterface()},
		AutoSystemProxy: true,
		SystemProxy:     systemProxy,
	})
	if err := session.Start(context.Background()); err == nil {
		t.Fatal("Start should fail")
	}
	if !systemProxy.active {
		t.Fatal("failed rollback state was lost")
	}
	systemProxy.restoreErr = nil
	if err := session.Stop(); err != nil {
		t.Fatalf("retry Stop: %v", err)
	}
	if systemProxy.active {
		t.Fatal("retry did not restore system proxy")
	}
}

func TestSessionRequiresInterfaces(t *testing.T) {
	session := NewSession(Config{Proxy: proxy.Config{HTTPAddr: "127.0.0.1:0"}})
	if err := session.Start(context.Background()); err == nil {
		t.Fatal("Start without interfaces should fail")
	}
}

func loopbackInterface() *netif.NetInterface {
	iface := &netif.NetInterface{Name: "loopback-test", IP: net.ParseIP("127.0.0.1")}
	iface.SetAlive(true)
	return iface
}
