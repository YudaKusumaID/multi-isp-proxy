package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/YudaKusumaID/multi-isp-proxy/internal/balancer"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/netif"
	"github.com/YudaKusumaID/multi-isp-proxy/internal/proxy"
)

// SystemProxy is the transactional system-proxy boundary used by a Session.
type SystemProxy interface {
	Begin(string) error
	Restore() error
	Active() bool
}

// Config contains one proxy session's runtime choices.
type Config struct {
	Proxy           proxy.Config
	Mode            balancer.Mode
	Interfaces      []*netif.NetInterface
	AutoSystemProxy bool
	SystemProxy     SystemProxy
	HealthInterval  time.Duration
}

// Session owns all resources created after the user confirms startup.
type Session struct {
	mu sync.Mutex

	config        Config
	server        *proxy.Server
	runtimeCancel context.CancelFunc
	started       bool
	serverStopped bool
	stopped       bool
}

// NewSession creates a session without changing external state.
func NewSession(config Config) *Session {
	if config.HealthInterval <= 0 {
		config.HealthInterval = 5 * time.Second
	}
	return &Session{config: config}
}

// Start first makes the listeners ready, then transactionally enables the
// optional system proxy, and finally starts managed health monitoring.
func (s *Session) Start(parent context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errors.New("application session already started")
	}
	if len(s.config.Interfaces) == 0 {
		return errors.New("at least one network interface is required")
	}

	runtimeCtx, runtimeCancel := context.WithCancel(parent)
	server := proxy.NewServerWithConfig(
		s.config.Proxy,
		balancer.New(s.config.Mode, s.config.Interfaces),
	)
	if err := server.Start(runtimeCtx); err != nil {
		runtimeCancel()
		return err
	}

	s.started = true
	s.server = server
	s.runtimeCancel = runtimeCancel

	if s.config.AutoSystemProxy {
		if s.config.SystemProxy == nil {
			s.stopServerLocked()
			return errors.New("system proxy manager is unavailable")
		}
		if err := s.config.SystemProxy.Begin(server.HTTPAddr()); err != nil {
			s.stopServerLocked()
			restoreErr := s.config.SystemProxy.Restore()
			if restoreErr == nil {
				s.stopped = true
			}
			return errors.Join(err, wrap("retry system proxy rollback", restoreErr))
		}
	}

	go netif.Monitor(runtimeCtx, s.config.Interfaces, s.config.HealthInterval)
	return nil
}

func (s *Session) stopServerLocked() {
	if s.runtimeCancel != nil {
		s.runtimeCancel()
	}
	if s.server != nil && !s.serverStopped {
		s.server.Stop()
		s.serverStopped = true
	}
}

// Stop is idempotent. A failed registry restore remains retryable.
func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopServerLocked()
	if s.config.AutoSystemProxy && s.config.SystemProxy != nil && s.config.SystemProxy.Active() {
		if err := s.config.SystemProxy.Restore(); err != nil {
			return err
		}
	}
	s.stopped = true
	return nil
}

// Wait blocks until the proxy server stops or fails.
func (s *Session) Wait() error {
	s.mu.Lock()
	server := s.server
	s.mu.Unlock()
	if server == nil {
		return errors.New("application session has not started")
	}
	return server.Wait()
}

// Stats returns the current connection counters.
func (s *Session) Stats() proxy.Stats {
	s.mu.Lock()
	server := s.server
	s.mu.Unlock()
	if server == nil {
		return proxy.Stats{}
	}
	return server.GetStats()
}

// Socks5Addr returns the active optional SOCKS5 listener.
func (s *Session) Socks5Addr() string {
	s.mu.Lock()
	server := s.server
	s.mu.Unlock()
	if server == nil {
		return ""
	}
	return server.Socks5Addr()
}

// HTTPAddr returns the active HTTP listener address.
func (s *Session) HTTPAddr() string {
	s.mu.Lock()
	server := s.server
	s.mu.Unlock()
	if server == nil {
		return s.config.Proxy.HTTPAddr
	}
	return server.HTTPAddr()
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
