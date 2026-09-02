# Multi ISP Proxy

[![CI](https://github.com/YudaKusumaID/multi-isp-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/YudaKusumaID/multi-isp-proxy/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/YudaKusumaID/multi-isp-proxy)](https://github.com/YudaKusumaID/multi-isp-proxy/releases)
[![License](https://img.shields.io/github/license/YudaKusumaID/multi-isp-proxy)](LICENSE)

Multi ISP Proxy is a local HTTP and SOCKS5 proxy that distributes new outbound
connections across selected network interfaces. It is intended for machines
with multiple active internet connections, such as Wi-Fi, Ethernet, or mobile
tethering.

Windows is the primary supported platform. Linux support is experimental and
is validated by GitHub Actions on Ubuntu; Linux system-proxy configuration is
still manual.

## What it does

- Runs an HTTP proxy on `127.0.0.1:1080` by default.
- Runs a SOCKS5 proxy on the next port, `127.0.0.1:1081` by default, when that
  port is available.
- Selects an outbound interface using round-robin or failover mode.
- Can configure and restore the current-user Windows HTTP proxy.
- Displays interface health, traffic counters, and connection counts in a TUI.

Load balancing happens per connection, not per packet. A single TCP connection
uses one interface and will not migrate seamlessly if that interface goes
offline. Multi-connection applications can use more than one interface, but
this is not equivalent to network-layer bonding.

## Platform status

| Platform | Status | System proxy |
| --- | --- | --- |
| Windows 10/11 | Primary | Optional automatic setup and restore |
| Ubuntu/Linux | Experimental | Manual application configuration |

SOCKS5 TCP is supported. SOCKS5 UDP is experimental and should be verified with
the target application before relying on it.

## Installation

Download one of these assets from the
[latest release](https://github.com/YudaKusumaID/multi-isp-proxy/releases):

- `multi-isp-proxy_windows_amd64.exe`
- `multi-isp-proxy_linux_amd64`
- `checksums.txt`

Verify the downloaded binary against `checksums.txt` before running it.

## Build from source

Go uses the version declared in [`go.mod`](go.mod).

```sh
git clone https://github.com/YudaKusumaID/multi-isp-proxy.git
cd multi-isp-proxy
go mod download
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force dist | Out-Null
go build -o dist/multi-isp-proxy.exe ./cmd/multi-isp-proxy
.\dist\multi-isp-proxy.exe
```

Linux:

```sh
mkdir -p dist
go build -o dist/multi-isp-proxy ./cmd/multi-isp-proxy
./dist/multi-isp-proxy
```

Build output belongs in `dist/`, which is ignored by Git.

## Usage

```text
multi-isp-proxy [-addr 127.0.0.1:1080] [-allow-remote] [-auth-file PATH] [-version]
```

1. Select one or more interfaces.
2. Choose round-robin or failover mode.
3. On Windows, choose whether the application may configure the current-user
   HTTP proxy. On Linux, configure the browser or application manually.
4. Press `r` to reset displayed counters or `q` to stop and restore settings.

Use a complete loopback address when changing the port:

```powershell
.\dist\multi-isp-proxy.exe -addr 127.0.0.1:8080
```

Loopback is enforced by default. A wildcard, LAN, or other non-loopback address
is rejected unless `-allow-remote` is supplied. Remote mode also requires
non-empty credentials, shared by HTTP Basic proxy authentication and SOCKS5
username/password authentication.

Prefer an environment variable so the secret is not exposed in command-line
history or process arguments:

```powershell
$env:MULTI_ISP_PROXY_AUTH = "proxy-user:use-a-long-random-password"
.\dist\multi-isp-proxy.exe -addr 0.0.0.0:1080 -allow-remote
```

Alternatively, put `username:password` in a user-readable credential file and
pass `-auth-file PATH`. Do not use both methods. Remote mode should also be
restricted with the operating-system firewall; it is not intended to be an
internet-facing public proxy. See [SECURITY.md](SECURITY.md).

## Logs

New runs write `multi-isp-proxy.log` below the operating system's user cache
directory instead of the repository root. At 5 MiB, it rotates to
`multi-isp-proxy.log.1`:

- Windows: `%LocalAppData%\multi-isp-proxy\multi-isp-proxy.log`
- Linux: `$XDG_CACHE_HOME/multi-isp-proxy/multi-isp-proxy.log`, or
  `~/.cache/multi-isp-proxy/multi-isp-proxy.log`

Existing `venn.log` and old binaries may be kept under `dist/legacy/`; they are
not used by the new build workflow.

## Crash recovery and single instance

Only one instance may run per user. Before changing Windows proxy settings, the
application writes a recovery journal below the user configuration directory.
After power loss or hard termination, the next launch restores the saved values
before opening the TUI. If the current settings were changed externally after
the crash, recovery refuses to overwrite them and preserves the journal for
manual inspection.

Typical Windows files:

- `%AppData%\multi-isp-proxy\instance.lock`
- `%AppData%\multi-isp-proxy\proxy-recovery.json`

Do not delete the recovery journal while Multi ISP Proxy controls the Windows
proxy. For conflict recovery guidance, see [SECURITY.md](SECURITY.md).

## Architecture

Runtime ownership, dependency direction, lifecycle invariants, and protocol
boundaries are documented in [docs/architecture.md](docs/architecture.md).

## Development

Run the local verification commands before opening a pull request:

```sh
gofmt -w .
go mod tidy
go vet ./...
go test -race ./...
```

CI runs formatting, module consistency, vet, tests, and the race detector on
Linux, plus native tests on Windows. Tagging a commit with a semantic version
such as `v1.2.0` runs the same CI before producing release binaries.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Linux support was initially contributed by
[@anugrahiyyan](https://github.com/anugrahiyyan).

## License

Licensed under the [MIT License](LICENSE).
