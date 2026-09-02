# Architecture

Multi ISP Proxy is organized around resource ownership rather than UI screens.
The terminal UI selects configuration; it does not directly manage listeners,
health goroutines, or registry transactions.

## Dependency direction

```text
cmd/multi-isp-proxy
├── internal/instance      per-user single-instance lease
├── internal/sysproxy      persistent Windows proxy transaction
└── internal/tui           terminal state and rendering
    └── internal/app       runtime session ownership
        ├── internal/proxy HTTP CONNECT, HTTP forward, SOCKS5
        ├── internal/balancer
        ├── internal/netif discovery, health, counters
        └── internal/sysproxy
```

Platform-specific code is limited to build-tagged files in `internal/netif`,
`internal/sysproxy`, and `internal/instance`. Windows is the primary platform;
Linux remains experimental and does not alter desktop proxy settings.

## Startup invariant

An `app.Session` starts in this order:

1. Validate configuration and selected interfaces.
2. Bind the HTTP listener synchronously; try the optional SOCKS5 listener.
3. Persist the original Windows settings in an atomic recovery journal.
4. Enable the Windows HTTP proxy.
5. Start cancellable interface health monitoring.

Therefore Windows is never pointed at a listener that has not successfully
bound. If a later step fails, the session closes listeners and retries the
registry rollback. A failed rollback remains owned by the session and can be
retried during final cleanup or recovered on the next process launch.

## Shutdown invariant

`Session.Stop` is idempotent. It cancels health monitoring, closes listeners and
active HTTP tunnels, restores the original Windows proxy values, then removes
the journal. The journal is removed only after a successful restore.

## Connection selection

Balancers return an ordered candidate list for each logical outbound dial.
Round-robin rotates the first candidate per connection; failover prefers the
highest-priority healthy interface. If a dial fails, the same request tries
later candidates within a bounded overall timeout. Interfaces marked down by a
probe remain last-resort candidates, preventing a blocked health endpoint from
causing a false total outage. A successful connection marks the path healthy.
Existing TCP connections remain on their original interface and cannot migrate.

## Security boundary

The default listener is loopback-only. Non-loopback listeners require an
explicit opt-in plus credentials. HTTP uses Basic proxy authentication; SOCKS5
uses username/password. Remote exposure should additionally be constrained by a
host firewall.

HTTP forwarding uses Go's `net/http` server and transport instead of manually
parsing one request per connection. Hop-by-hop and proxy authorization headers
are removed before forwarding. CONNECT tunnels preserve buffered client bytes,
support half-close, and are closed during session shutdown.

## Testing boundaries

- Balancer tests prove order and unavailable-interface behavior.
- Proxy tests cover address validation, authentication, failover retry, normal
  HTTP forwarding, and CONNECT tunneling over real loopback sockets.
- Session tests prove listener readiness precedes system proxy activation and
  rollback failures remain retryable.
- System proxy tests use a fake registry boundary to prove journaling and
  conflict-safe recovery on every platform.
- CI runs the race detector on Linux and native tests on Windows.
