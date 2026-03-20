package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/things-go/go-socks5"

	"github.com/ayanacorp/venn-combine-connection/internal/balancer"
	"github.com/ayanacorp/venn-combine-connection/internal/netif"
)

// Stats holds proxy server statistics.
type Stats struct {
	ActiveConnections int64
	TotalConnections  int64
}

// Server wraps both HTTP CONNECT and SOCKS5 proxy with multi-interface load balancing.
type Server struct {
	bal          balancer.Strategy
	httpAddr     string
	socks5Addr   string
	httpListener net.Listener
	socks5Server *socks5.Server
	socks5Ln     net.Listener
	stats        Stats
	mu           sync.RWMutex
	cancelCtx    context.CancelFunc
	ctx          context.Context
}

// NewServer creates a new proxy server that distributes connections via the balancer.
// httpAddr is used for the HTTP CONNECT proxy (Windows system proxy).
// SOCKS5 runs on httpAddr port + 1 as a secondary option.
func NewServer(httpAddr string, bal balancer.Strategy) *Server {
	s := &Server{
		bal:      bal,
		httpAddr: httpAddr,
	}

	// Parse HTTP addr to calculate SOCKS5 addr (port + 1)
	host, port, err := net.SplitHostPort(httpAddr)
	if err == nil {
		var portNum int
		fmt.Sscanf(port, "%d", &portNum)
		s.socks5Addr = fmt.Sprintf("%s:%d", host, portNum+1)
	}

	// Create SOCKS5 server
	socks5Server := socks5.NewServer(
		socks5.WithDial(s.dialWithBalancer),
		socks5.WithLogger(socks5.NewLogger(log.New(log.Writer(), "socks5: ", log.LstdFlags))),
	)
	s.socks5Server = socks5Server

	return s
}

// dialWithBalancer is the custom dialer that binds to a specific interface.
func (s *Server) dialWithBalancer(ctx context.Context, network, addr string) (net.Conn, error) {
	iface := s.bal.Next()
	if iface == nil {
		return nil, fmt.Errorf("no available network interface")
	}

	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		LocalAddr: &net.TCPAddr{IP: iface.IP},
	}

	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		log.Printf("[proxy] dial via %s to %s failed: %v", iface.IP, addr, err)
		return nil, fmt.Errorf("dial via %s failed: %w", iface.String(), err)
	}

	log.Printf("[proxy] connected via %s to %s", iface.IP, addr)

	// Track statistics
	atomic.AddInt64(&s.stats.TotalConnections, 1)
	atomic.AddInt64(&s.stats.ActiveConnections, 1)

	// Wrap connection to track when it closes
	return &trackedConn{
		Conn:  conn,
		iface: iface,
		onClose: func() {
			atomic.AddInt64(&s.stats.ActiveConnections, -1)
		},
	}, nil
}

// dialDirect creates a connection via the load balancer (used by HTTP proxy handler).
func (s *Server) dialDirect(network, addr string) (net.Conn, error) {
	return s.dialWithBalancer(context.Background(), network, addr)
}

// Start begins listening and serving both HTTP and SOCKS5 proxy.
func (s *Server) Start(ctx context.Context) error {
	s.ctx, s.cancelCtx = context.WithCancel(ctx)

	// Start HTTP CONNECT proxy (primary — used by Windows system proxy)
	httpLn, err := net.Listen("tcp", s.httpAddr)
	if err != nil {
		s.cancelCtx()
		return fmt.Errorf("failed to listen HTTP on %s: %w", s.httpAddr, err)
	}
	s.httpListener = httpLn
	log.Printf("[proxy] HTTP CONNECT proxy listening on %s", s.httpAddr)

	// Start SOCKS5 proxy (secondary)
	if s.socks5Addr != "" {
		socks5Ln, err := net.Listen("tcp", s.socks5Addr)
		if err != nil {
			log.Printf("[proxy] SOCKS5 listener failed on %s: %v (continuing without SOCKS5)", s.socks5Addr, err)
		} else {
			s.socks5Ln = socks5Ln
			log.Printf("[proxy] SOCKS5 proxy listening on %s", s.socks5Addr)
			go func() {
				if err := s.socks5Server.Serve(socks5Ln); err != nil && s.ctx.Err() == nil {
					log.Printf("[proxy] SOCKS5 server error: %v", err)
				}
			}()
		}
	}

	// Context cancellation cleanup
	go func() {
		<-s.ctx.Done()
		httpLn.Close()
		if s.socks5Ln != nil {
			s.socks5Ln.Close()
		}
	}()

	// Serve HTTP CONNECT proxy (blocking)
	return s.serveHTTP(httpLn)
}

// serveHTTP handles incoming HTTP proxy connections.
func (s *Server) serveHTTP(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return nil // Graceful shutdown
			}
			log.Printf("[proxy] accept error: %v", err)
			continue
		}
		go s.handleHTTPConnection(conn)
	}
}

// handleHTTPConnection processes a single HTTP proxy connection.
func (s *Server) handleHTTPConnection(clientConn net.Conn) {
	defer clientConn.Close()

	br := bufio.NewReader(clientConn)
	req, err := http.ReadRequest(br)
	if err != nil {
		log.Printf("[proxy] failed to read request: %v", err)
		return
	}

	if req.Method == http.MethodConnect {
		s.handleConnect(clientConn, req)
	} else {
		s.handleHTTPForward(clientConn, req, br)
	}
}

// handleConnect handles HTTPS tunneling via HTTP CONNECT.
func (s *Server) handleConnect(clientConn net.Conn, req *http.Request) {
	// Connect to the target via the load balancer
	targetConn, err := s.dialDirect("tcp", req.Host)
	if err != nil {
		fmt.Fprintf(clientConn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		log.Printf("[proxy] CONNECT to %s failed: %v", req.Host, err)
		return
	}
	defer targetConn.Close()

	// Send 200 Connection Established
	fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")

	// Bidirectional copy
	s.tunnel(clientConn, targetConn)
}

// handleHTTPForward forwards plain HTTP requests.
func (s *Server) handleHTTPForward(clientConn net.Conn, req *http.Request, br *bufio.Reader) {
	// Determine target host
	host := req.Host
	if !strings.Contains(host, ":") {
		host = host + ":80"
	}

	// Connect to target via load balancer
	targetConn, err := s.dialDirect("tcp", host)
	if err != nil {
		fmt.Fprintf(clientConn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		log.Printf("[proxy] HTTP forward to %s failed: %v", host, err)
		return
	}
	defer targetConn.Close()

	// Remove proxy headers
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")

	// Forward the request
	req.RequestURI = req.URL.Path
	if req.URL.RawQuery != "" {
		req.RequestURI += "?" + req.URL.RawQuery
	}

	if err := req.Write(targetConn); err != nil {
		log.Printf("[proxy] failed to forward request: %v", err)
		return
	}

	// Pipe response back
	s.tunnel(clientConn, targetConn)
}

// tunnel copies data bidirectionally between two connections.
func (s *Server) tunnel(client, target net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(target, client)
		if tc, ok := target.(*trackedConn); ok {
			tc.Conn.(*net.TCPConn).CloseWrite()
		}
		done <- struct{}{}
	}()

	go func() {
		io.Copy(client, target)
		if tc, ok := client.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	<-done
}

// Stop gracefully shuts down the proxy server.
func (s *Server) Stop() {
	if s.cancelCtx != nil {
		s.cancelCtx()
	}
}

// GetStats returns a copy of the current stats.
func (s *Server) GetStats() Stats {
	return Stats{
		ActiveConnections: atomic.LoadInt64(&s.stats.ActiveConnections),
		TotalConnections:  atomic.LoadInt64(&s.stats.TotalConnections),
	}
}

// Addr returns the HTTP proxy listen address.
func (s *Server) Addr() string {
	return s.httpAddr
}

// Socks5Addr returns the SOCKS5 proxy listen address.
func (s *Server) Socks5Addr() string {
	return s.socks5Addr
}

// trackedConn wraps a net.Conn to track bytes and connection lifecycle per interface.
type trackedConn struct {
	net.Conn
	iface   *netif.NetInterface
	onClose func()
	closed  bool
	mu      sync.Mutex
}

// Read counts bytes received through this interface.
func (c *trackedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && c.iface != nil {
		c.iface.AddBytesRecv(uint64(n))
	}
	return n, err
}

// Write counts bytes sent through this interface.
func (c *trackedConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 && c.iface != nil {
		c.iface.AddBytesSent(uint64(n))
	}
	return n, err
}

// Close closes the connection and invokes the onClose callback.
func (c *trackedConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		if c.onClose != nil {
			c.onClose()
		}
	}
	return c.Conn.Close()
}
