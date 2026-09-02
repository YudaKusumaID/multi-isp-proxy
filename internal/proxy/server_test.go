package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/balancer"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
)

func TestDeriveSOCKS5Addr(t *testing.T) {
	tests := []struct {
		name     string
		httpAddr string
		want     string
	}{
		{name: "IPv4", httpAddr: "127.0.0.1:1080", want: "127.0.0.1:1081"},
		{name: "IPv6", httpAddr: "[::1]:1080", want: "[::1]:1081"},
		{name: "wildcard", httpAddr: ":1080", want: ":1081"},
		{name: "dynamic port", httpAddr: "127.0.0.1:0", want: ""},
		{name: "maximum port", httpAddr: "127.0.0.1:65535", want: ""},
		{name: "invalid", httpAddr: "not-an-address", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveSOCKS5Addr(test.httpAddr); got != test.want {
				t.Fatalf("deriveSOCKS5Addr(%q) = %q, want %q", test.httpAddr, got, test.want)
			}
		})
	}
}

func TestListenerSecurity(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{name: "IPv4 loopback", config: Config{HTTPAddr: "127.0.0.1:1080"}},
		{name: "IPv6 loopback", config: Config{HTTPAddr: "[::1]:1080"}},
		{name: "wildcard denied", config: Config{HTTPAddr: ":1080"}, wantErr: true},
		{name: "remote denied", config: Config{HTTPAddr: "192.0.2.1:1080"}, wantErr: true},
		{name: "remote requires auth", config: Config{HTTPAddr: "0.0.0.0:1080", AllowRemote: true}, wantErr: true},
		{name: "remote authenticated", config: Config{HTTPAddr: "0.0.0.0:1080", AllowRemote: true, Username: "user", Password: "secret"}},
		{name: "invalid", config: Config{HTTPAddr: "broken"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateListenerSecurity(test.config)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateListenerSecurity() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDialRetriesNextInterface(t *testing.T) {
	first := &netif.NetInterface{Name: "first", IP: net.ParseIP("192.0.2.1")}
	second := &netif.NetInterface{Name: "second", IP: net.ParseIP("192.0.2.2")}
	first.SetAlive(true)
	second.SetAlive(true)
	server := NewServer("127.0.0.1:0", balancer.NewFailover([]*netif.NetInterface{first, second}))

	peer, successful := net.Pipe()
	defer peer.Close()
	var attempts []string
	server.dial = func(_ context.Context, _, _ string, local net.Addr) (net.Conn, error) {
		attempts = append(attempts, local.String())
		if len(attempts) == 1 {
			return nil, errors.New("simulated ISP failure")
		}
		return successful, nil
	}

	conn, err := server.dialWithBalancer(context.Background(), "tcp", "example.test:443")
	if err != nil {
		t.Fatalf("dialWithBalancer: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempt count = %d, want 2", len(attempts))
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close connection: %v", err)
	}
	stats := server.GetStats()
	if stats.TotalConnections != 1 || stats.ActiveConnections != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestHTTPForwardingIntegration(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Target", "yes")
		_, _ = io.WriteString(w, "forwarded "+req.URL.Path)
	}))
	defer target.Close()

	server := startLoopbackServer(t)
	proxyURL, err := url.Parse("http://" + server.HTTPAddr())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	response, err := client.Get(target.URL + "/through-proxy")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "forwarded /through-proxy" || response.Header.Get("X-Target") != "yes" {
		t.Fatalf("unexpected response: status=%d header=%q body=%q", response.StatusCode, response.Header.Get("X-Target"), body)
	}
}

func TestCONNECTTunnelIntegration(t *testing.T) {
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoListener.Close()
	go func() {
		conn, acceptErr := echoListener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()

	server := startLoopbackServer(t)
	conn, err := net.DialTimeout("tcp", server.HTTPAddr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, "CONNECT "+echoListener.Addr().String()+" HTTP/1.1\r\nHost: "+echoListener.Addr().String()+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", response.StatusCode)
	}
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "ping" {
		t.Fatalf("echo = %q", buffer)
	}
}

func TestProxyAuthentication(t *testing.T) {
	server := NewServerWithConfig(Config{
		HTTPAddr: "127.0.0.1:0",
		Username: "alice",
		Password: "correct horse",
	}, balancer.NewFailover(nil))

	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	if server.authorized(request) {
		t.Fatal("request without credentials was authorized")
	}
	request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:wrong")))
	if server.authorized(request) {
		t.Fatal("request with wrong credentials was authorized")
	}
	request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:correct horse")))
	if !server.authorized(request) {
		t.Fatal("request with valid credentials was rejected")
	}

	recorder := httptest.NewRecorder()
	unauthorized := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	server.ServeHTTP(recorder, unauthorized)
	if recorder.Code != http.StatusProxyAuthRequired || !strings.Contains(recorder.Header().Get("Proxy-Authenticate"), "Basic") {
		t.Fatalf("unauthorized response = %d, header %q", recorder.Code, recorder.Header().Get("Proxy-Authenticate"))
	}
}

func TestNormalizeTarget(t *testing.T) {
	tests := map[string]string{
		"example.test":      "example.test:443",
		"example.test:8443": "example.test:8443",
		"2001:db8::1":       "[2001:db8::1]:443",
		"[2001:db8::1]:443": "[2001:db8::1]:443",
	}
	for input, expected := range tests {
		actual, err := normalizeTarget(input, "443")
		if err != nil || actual != expected {
			t.Fatalf("normalizeTarget(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
	if _, err := normalizeTarget("", "443"); err == nil {
		t.Fatal("empty target did not fail")
	}
}

func startLoopbackServer(t *testing.T) *Server {
	t.Helper()
	iface := &netif.NetInterface{Name: "loopback-test", IP: net.ParseIP("127.0.0.1")}
	iface.SetAlive(true)
	server := NewServer("127.0.0.1:0", balancer.NewFailover([]*netif.NetInterface{iface}))
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	t.Cleanup(server.Stop)
	return server
}

func TestLocalAddrForNetwork(t *testing.T) {
	ip := net.ParseIP("192.0.2.10")

	tcpAddr, err := localAddrForNetwork("tcp", ip)
	if err != nil {
		t.Fatalf("TCP local address: %v", err)
	}
	if _, ok := tcpAddr.(*net.TCPAddr); !ok {
		t.Fatalf("TCP local address has type %T", tcpAddr)
	}

	udpAddr, err := localAddrForNetwork("udp4", ip)
	if err != nil {
		t.Fatalf("UDP local address: %v", err)
	}
	if _, ok := udpAddr.(*net.UDPAddr); !ok {
		t.Fatalf("UDP local address has type %T", udpAddr)
	}

	if _, err := localAddrForNetwork("unix", ip); err == nil {
		t.Fatal("unsupported network returned no error")
	}
}
