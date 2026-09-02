# Security Policy

## Supported versions

Security fixes are applied to the latest release and the default branch.

## Reporting a vulnerability

Please use GitHub's private security advisory reporting for this repository.
Do not publish credentials, recovery journals, logs containing private network
details, or a working exploit in a public issue.

## Safe deployment

The default `127.0.0.1` listener is the recommended configuration. Binding to a
LAN or wildcard address requires `-allow-remote` and credentials supplied by
`MULTI_ISP_PROXY_AUTH` or `-auth-file`. Use a long unique password and restrict
the listening port with Windows Firewall or the Linux firewall.

Proxy authentication protects access to the listener but does not turn this
application into a hardened public relay. There is no multi-user authorization,
rate limiting, TLS termination for the proxy connection, or centralized secret
management. Do not expose it directly to the internet.

## Windows recovery journal

The recovery journal contains the current user's previous Windows proxy values
and is created with user-only file permissions where the operating system
supports POSIX-style modes. It exists only while an automatic proxy transaction
needs recovery.

On startup, recovery proceeds only when current values match either the saved
backup or values managed by Multi ISP Proxy. If another program or the user
changed them, startup stops instead of overwriting those changes. Inspect the
journal and Windows **Settings > Network & internet > Proxy**, restore the
desired values manually, then remove the journal only after confirming no
Multi ISP Proxy process is running.

Typical path:

```text
%AppData%\multi-isp-proxy\proxy-recovery.json
```

Logs contain interface addresses and destination hosts. Treat them as private
diagnostic data.
