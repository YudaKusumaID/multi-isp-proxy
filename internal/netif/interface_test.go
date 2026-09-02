package netif

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestMonitorChecksImmediatelyAndStops(t *testing.T) {
	first := &NetInterface{Name: "first", IP: net.ParseIP("192.0.2.1")}
	second := &NetInterface{Name: "second", IP: net.ParseIP("192.0.2.2")}
	first.SetAlive(true)
	second.SetAlive(true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	checked := make(chan string, 2)
	go func() {
		MonitorWithChecker(ctx, []*NetInterface{first, second}, time.Hour, func(_ context.Context, ni *NetInterface) bool {
			checked <- ni.Name
			return ni == second
		})
		close(done)
	}()

	for range 2 {
		select {
		case <-checked:
		case <-time.After(time.Second):
			t.Fatal("initial health check did not run immediately")
		}
	}
	if first.IsAlive() || !second.IsAlive() {
		t.Fatalf("unexpected health state: first=%v second=%v", first.IsAlive(), second.IsAlive())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop after cancellation")
	}
}
