package proxy

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/things-go/go-socks5"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/balancer"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
)

const (
	defaultDialTimeout        = 15 * time.Second
	defaultDialAttemptTimeout = 5 * time.Second
	defaultReadHeaderTimeout  = 10 * time.Second
	defaultIdleTimeout        = 90 * time.Second
	defaultResponseTimeout    = 30 * time.Second
)

// Stats holds proxy server statistics.
type Stats struct {
	ActiveConnections int64
	TotalConnections  int64
}

// Config controls listener security and network timeouts.
type Config struct {
	HTTPAddr              string
	AllowRemote           bool
	Username              string
	Password              string
	DialTimeout           time.Duration
	DialAttemptTimeout    time.Duration
	ReadHeaderTimeout     time.Duration
	IdleTimeout           time.Duration
	ResponseHeaderTimeout time.Duration
}

// Server serves HTTP CONNECT, plain HTTP, and SOCKS5 through a balancer.
// A Server is single-use: create a new instance after Stop.
type Server struct {
	config Config
	bal    balancer.Strategy

	socks5Addr   string
	socks5Server *socks5.Server
	httpServer   *http.Server
	transport    *http.Transport
	httpLn       net.Listener
	socks5Ln     net.Listener

	stats Stats

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
	runErr  error
	active  map[net.Conn]struct{}
	dial    func(context.Context, string, string, net.Addr) (net.Conn, error)
}

// NewServer creates a loopback-only server with safe timeout defaults.
func NewServer(httpAddr string, bal balancer.Strategy) *Server {
	return NewServerWithConfig(Config{HTTPAddr: httpAddr}, bal)
}

// NewServerWithConfig creates a server with explicit listener settings.
func NewServerWithConfig(config Config, bal balancer.Strategy) *Server {
	if config.DialTimeout <= 0 {
		config.DialTimeout = defaultDialTimeout
	}
	if config.DialAttemptTimeout <= 0 || config.DialAttemptTimeout > config.DialTimeout {
		config.DialAttemptTimeout = min(defaultDialAttemptTimeout, config.DialTimeout)
	}
	if config.ReadHeaderTimeout <= 0 {
		config.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = defaultIdleTimeout
	}
	if config.ResponseHeaderTimeout <= 0 {
		config.ResponseHeaderTimeout = defaultResponseTimeout
	}

	s := &Server{
		config: config,
		bal:    bal,
		active: make(map[net.Conn]struct{}),
		dial: func(ctx context.Context, network, addr string, localAddr net.Addr) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   config.DialAttemptTimeout,
				KeepAlive: 30 * time.Second,
				LocalAddr: localAddr,
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}

	options := []socks5.Option{
		socks5.WithDial(s.dialWithBalancer),
		socks5.WithLogger(socks5.NewLogger(log.New(log.Writer(), "socks5: ", log.LstdFlags))),
	}
	if config.Username != "" || config.Password != "" {
		credentials := socks5.StaticCredentials{config.Username: config.Password}
		options = append(options,
			socks5.WithCredential(credentials),
			socks5.WithAuthMethods([]socks5.Authenticator{
				socks5.UserPassAuthenticator{Credentials: credentials},
			}),
		)
	}
	s.socks5Server = socks5.NewServer(options...)
	return s
}

// Start binds listeners synchronously before returning. This readiness
// guarantee lets callers change system proxy settings only after the proxy is
// actually reachable.
func (s *Server) Start(parent context.Context) error {
	if err := validateListenerSecurity(s.config); err != nil {
		return err
	}
	if s.bal == nil {
		return errors.New("proxy balancer is required")
	}
	if err := parent.Err(); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("proxy server already started")
	}
	s.started = true
	s.mu.Unlock()

	httpLn, err := net.Listen("tcp", s.config.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP on %s: %w", s.config.HTTPAddr, err)
	}

	socksAddr := deriveSOCKS5Addr(s.config.HTTPAddr)
	var socksLn net.Listener
	if socksAddr != "" {
		socksLn, err = net.Listen("tcp", socksAddr)
		if err != nil {
			log.Printf("[proxy] SOCKS5 listener unavailable on %s: %v", socksAddr, err)
			socksAddr = ""
		}
	}

	ctx, cancel := context.WithCancel(parent)
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           s.dialWithBalancer,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       s.config.IdleTimeout,
		ResponseHeaderTimeout: s.config.ResponseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
	httpServer := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: s.config.ReadHeaderTimeout,
		IdleTimeout:       s.config.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          log.New(log.Writer(), "http-proxy: ", log.LstdFlags),
	}

	s.mu.Lock()
	s.cancel = cancel
	s.done = make(chan struct{})
	s.httpLn = httpLn
	s.socks5Ln = socksLn
	s.socks5Addr = socksAddr
	s.transport = transport
	s.httpServer = httpServer
	done := s.done
	s.mu.Unlock()

	log.Printf("[proxy] HTTP proxy listening on %s", httpLn.Addr())
	if socksLn != nil {
		log.Printf("[proxy] SOCKS5 proxy listening on %s", socksLn.Addr())
		go func() {
			if serveErr := s.socks5Server.Serve(socksLn); serveErr != nil && ctx.Err() == nil {
				log.Printf("[proxy] SOCKS5 server error: %v", serveErr)
			}
		}()
	}

	go func() {
		serveErr := httpServer.Serve(httpLn)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && ctx.Err() == nil {
			s.setRunError(fmt.Errorf("HTTP proxy server: %w", serveErr))
		}
		cancel()
		close(done)
	}()

	go func() {
		<-ctx.Done()
		s.closeResources()
	}()

	return nil
}

// Wait blocks until the primary HTTP server exits.
func (s *Server) Wait() error {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return errors.New("proxy server has not started")
	}
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runErr
}

// Stop closes listeners and active tunnels, then waits for the HTTP server.
func (s *Server) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	s.closeResources()
	if done != nil {
		<-done
	}
}

func (s *Server) closeResources() {
	s.mu.Lock()
	httpServer := s.httpServer
	httpLn := s.httpLn
	socksLn := s.socks5Ln
	transport := s.transport
	connections := make([]net.Conn, 0, len(s.active))
	for conn := range s.active {
		connections = append(connections, conn)
	}
	s.mu.Unlock()

	if httpServer != nil {
		_ = httpServer.Close()
	} else if httpLn != nil {
		_ = httpLn.Close()
	}
	if socksLn != nil {
		_ = socksLn.Close()
	}
	if transport != nil {
		transport.CloseIdleConnections()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (s *Server) setRunError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runErr == nil {
		s.runErr = err
	}
}

// ServeHTTP implements both CONNECT tunneling and normal HTTP forwarding.
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !s.authorized(req) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="multi-isp-proxy"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}

	if req.Method == http.MethodConnect {
		s.handleConnect(w, req)
		return
	}
	s.handleForward(w, req)
}

func (s *Server) authorized(req *http.Request) bool {
	if s.config.Username == "" && s.config.Password == "" {
		return true
	}

	value := req.Header.Get("Proxy-Authorization")
	scheme, encoded, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return false
	}
	usernameOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.config.Username))
	passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte(s.config.Password))
	return usernameOK&passwordOK == 1
}

func (s *Server) handleConnect(w http.ResponseWriter, req *http.Request) {
	target, err := normalizeTarget(req.Host, "443")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	targetConn, err := s.dialWithBalancer(req.Context(), "tcp", target)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		log.Printf("[proxy] CONNECT to %s failed: %v", target, err)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = targetConn.Close()
		http.Error(w, "CONNECT unsupported", http.StatusInternalServerError)
		return
	}
	clientConn, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = targetConn.Close()
		return
	}
	client := &bufferedConn{Conn: clientConn, reader: buffered.Reader}
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = client.Close()
		_ = targetConn.Close()
		return
	}
	if err := buffered.Flush(); err != nil {
		_ = client.Close()
		_ = targetConn.Close()
		return
	}

	s.track(client)
	s.track(targetConn)
	defer s.untrack(client)
	defer s.untrack(targetConn)
	defer client.Close()
	defer targetConn.Close()
	s.tunnel(client, targetConn)
}

func (s *Server) handleForward(w http.ResponseWriter, req *http.Request) {
	if req.URL.Scheme == "" {
		req.URL.Scheme = "http"
	}
	if req.URL.Host == "" {
		req.URL.Host = req.Host
	}
	if !strings.EqualFold(req.URL.Scheme, "http") {
		http.Error(w, "use CONNECT for non-HTTP destinations", http.StatusBadRequest)
		return
	}

	outbound := req.Clone(req.Context())
	outbound.RequestURI = ""
	outbound.Header = req.Header.Clone()
	removeHopByHopHeaders(outbound.Header)
	outbound.Header.Del("Proxy-Authorization")

	response, err := s.transport.RoundTrip(outbound)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		log.Printf("[proxy] HTTP forward to %s failed: %v", req.URL.Host, err)
		return
	}
	defer response.Body.Close()
	removeHopByHopHeaders(response.Header)
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		log.Printf("[proxy] copying HTTP response failed: %v", err)
	}
}

func removeHopByHopHeaders(header http.Header) {
	for _, token := range strings.Split(header.Get("Connection"), ",") {
		if token = strings.TrimSpace(token); token != "" {
			header.Del(token)
		}
	}
	for _, name := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(name)
	}
}

func copyHeaders(destination, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func normalizeTarget(host, defaultPort string) (string, error) {
	if host == "" {
		return "", errors.New("missing destination host")
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host, nil
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return net.JoinHostPort(ip.String(), defaultPort), nil
	}
	if strings.Contains(host, ":") {
		return "", fmt.Errorf("invalid destination host %q", host)
	}
	return net.JoinHostPort(host, defaultPort), nil
}

// dialWithBalancer retries every candidate supplied by the strategy.
func (s *Server) dialWithBalancer(ctx context.Context, network, addr string) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, s.config.DialTimeout)
	defer cancel()

	candidates := s.bal.Candidates()
	if len(candidates) == 0 {
		return nil, errors.New("no available network interface")
	}

	var failures []error
	for _, iface := range candidates {
		if err := dialCtx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		localAddr, err := localAddrForNetwork(network, iface.IP)
		if err != nil {
			return nil, err
		}
		attemptCtx, attemptCancel := context.WithTimeout(dialCtx, s.config.DialAttemptTimeout)
		conn, err := s.dial(attemptCtx, network, addr, localAddr)
		attemptCancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", iface.String(), err))
			log.Printf("[proxy] dial via %s to %s failed: %v", iface.IP, addr, err)
			continue
		}

		log.Printf("[proxy] connected via %s to %s", iface.IP, addr)
		iface.SetAlive(true)
		atomic.AddInt64(&s.stats.TotalConnections, 1)
		atomic.AddInt64(&s.stats.ActiveConnections, 1)
		return &trackedConn{
			Conn:  conn,
			iface: iface,
			onClose: func() {
				atomic.AddInt64(&s.stats.ActiveConnections, -1)
			},
		}, nil
	}
	return nil, fmt.Errorf("all network interfaces failed for %s: %w", addr, errors.Join(failures...))
}

func deriveSOCKS5Addr(httpAddr string) string {
	host, port, err := net.SplitHostPort(httpAddr)
	if err != nil {
		return ""
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber >= 65535 {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(portNumber+1))
}

func localAddrForNetwork(network string, ip net.IP) (net.Addr, error) {
	switch {
	case strings.HasPrefix(network, "tcp"):
		return &net.TCPAddr{IP: ip}, nil
	case strings.HasPrefix(network, "udp"):
		return &net.UDPAddr{IP: ip}, nil
	default:
		return nil, fmt.Errorf("unsupported network %q", network)
	}
}

func validateListenerSecurity(config Config) error {
	host, _, err := net.SplitHostPort(config.HTTPAddr)
	if err != nil {
		return fmt.Errorf("invalid proxy address %q: %w", config.HTTPAddr, err)
	}
	loopback := strings.EqualFold(host, "localhost")
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		loopback = ip.IsLoopback()
	}
	if loopback {
		return nil
	}
	if !config.AllowRemote {
		return fmt.Errorf("non-loopback address %q requires --allow-remote", config.HTTPAddr)
	}
	if config.Username == "" || config.Password == "" {
		return errors.New("remote proxy access requires both username and password")
	}
	return nil
}

func (s *Server) tunnel(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	copyOneWay := func(destination, source net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(destination, source)
		closeWrite(destination)
	}
	go copyOneWay(target, client)
	go copyOneWay(client, target)
	wg.Wait()
}

func closeWrite(conn net.Conn) {
	if halfCloser, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = halfCloser.CloseWrite()
	}
}

func (s *Server) track(conn net.Conn) {
	s.mu.Lock()
	s.active[conn] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	delete(s.active, conn)
	s.mu.Unlock()
}

// GetStats returns a snapshot of proxy connection statistics.
func (s *Server) GetStats() Stats {
	return Stats{
		ActiveConnections: atomic.LoadInt64(&s.stats.ActiveConnections),
		TotalConnections:  atomic.LoadInt64(&s.stats.TotalConnections),
	}
}

// Socks5Addr returns the active SOCKS5 listener address, if available.
func (s *Server) Socks5Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.socks5Addr
}

// HTTPAddr returns the active HTTP listener address, or the configured address
// before startup.
func (s *Server) HTTPAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpLn != nil {
		return s.httpLn.Addr().String()
	}
	return s.config.HTTPAddr
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(buffer []byte) (int, error) {
	return c.reader.Read(buffer)
}

func (c *bufferedConn) CloseWrite() error {
	if halfCloser, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return halfCloser.CloseWrite()
	}
	return nil
}

// trackedConn tracks bytes and connection lifecycle for an interface.
type trackedConn struct {
	net.Conn
	iface   *netif.NetInterface
	onClose func()
	closed  bool
	mu      sync.Mutex
}

func (c *trackedConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if n > 0 && c.iface != nil {
		c.iface.AddBytesRecv(uint64(n))
	}
	return n, err
}

func (c *trackedConn) Write(buffer []byte) (int, error) {
	n, err := c.Conn.Write(buffer)
	if n > 0 && c.iface != nil {
		c.iface.AddBytesSent(uint64(n))
	}
	return n, err
}

func (c *trackedConn) CloseWrite() error {
	if halfCloser, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return halfCloser.CloseWrite()
	}
	return nil
}

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
