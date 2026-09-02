package tui

import (
	"net"
	"strings"
	"testing"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/balancer"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/proxy"
)

func TestInterfaceSelectionAndModeFlow(t *testing.T) {
	model := testModel()

	updated, _ := model.handleSelectInterfaces("space")
	model = updated.(Model)
	if !model.selected[0] || model.countSelected() != 1 {
		t.Fatalf("first interface was not selected: %#v", model.selected)
	}
	updated, _ = model.handleSelectInterfaces("down")
	model = updated.(Model)
	updated, _ = model.handleSelectInterfaces("space")
	model = updated.(Model)
	updated, _ = model.handleSelectInterfaces("enter")
	model = updated.(Model)
	if model.phase != PhaseSelectMode {
		t.Fatalf("phase = %v, want mode selection", model.phase)
	}

	updated, _ = model.handleSelectMode("tab")
	model = updated.(Model)
	if model.mode != balancer.ModeFailover {
		t.Fatalf("mode = %v, want failover", model.mode)
	}
	updated, _ = model.handleSelectMode("enter")
	model = updated.(Model)
	if model.phase != PhaseConfirmWinProxy {
		t.Fatalf("phase = %v, want confirmation", model.phase)
	}
}

func TestManualProxySessionStartsAndCleansUp(t *testing.T) {
	model := testModel()
	model.selected[0] = true
	model.mode = balancer.ModeFailover
	model.phase = PhaseConfirmWinProxy
	initialCopy := model

	updated, command := model.startProxy()
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("startProxy: %v", model.err)
	}
	if model.phase != PhaseRunning || model.session == nil || command == nil {
		t.Fatalf("session did not enter running phase: %+v", model)
	}
	if !strings.Contains(model.proxyAddr, "127.0.0.1:") || strings.HasSuffix(model.proxyAddr, ":0") {
		t.Fatalf("proxy address was not resolved: %q", model.proxyAddr)
	}
	if err := initialCopy.Cleanup(); err != nil {
		t.Fatalf("Cleanup through initial model copy: %v", err)
	}
	if err := model.Cleanup(); err != nil {
		t.Fatalf("second Cleanup: %v", err)
	}
}

func TestViewCoversSetupAndErrorStates(t *testing.T) {
	model := testModel()
	if content := model.View().Content; !strings.Contains(content, "Select Network Interfaces") {
		t.Fatalf("selection view missing title: %q", content)
	}
	model.phase = PhaseSelectMode
	if content := model.View().Content; !strings.Contains(content, "Round-Robin") {
		t.Fatalf("mode view missing option: %q", content)
	}
	model.phase = PhaseStopping
	if content := model.View().Content; !strings.Contains(content, "Proxy server stopped") {
		t.Fatalf("stopping view missing status: %q", content)
	}
}

func testModel() Model {
	first := &netif.NetInterface{Name: "first", FriendlyName: "First ISP", IP: net.ParseIP("127.0.0.1")}
	second := &netif.NetInterface{Name: "second", FriendlyName: "Second ISP", IP: net.ParseIP("127.0.0.2")}
	first.SetAlive(true)
	second.SetAlive(true)
	return Model{
		phase:       PhaseSelectInterfaces,
		allIfaces:   []*netif.NetInterface{first, second},
		selected:    make(map[int]bool),
		mode:        balancer.ModeRoundRobin,
		proxyAddr:   "127.0.0.1:0",
		proxyConfig: proxy.Config{HTTPAddr: "127.0.0.1:0"},
		lifecycle:   &lifecycleState{},
	}
}
