# Architecture

## Overview

sstop is structured as a pipeline:

```
Platform (OS-specific) → Collector (aggregation) → UI (Bubble Tea TUI)
```

```
┌─────────────────────────────────────────────────────────┐
│                        main.go                          │
│  Platform → Collector → Bubble Tea Program              │
└─────────────────────────────────────────────────────────┘
         │            │              │
         ▼            ▼              ▼
┌──────────────┐ ┌──────────┐ ┌────────────────────┐
│   platform/  │ │collector/│ │        ui/         │
│              │ │          │ │                    │
│ linux.go     │ │collector │ │ app.go (Model)     │
│ linux_proc.go│ │bandwidth │ │ process_table.go   │
│ linux_pcap.go│ │dns.go    │ │ process_detail.go  │
│ darwin.go    │ │history.go│ │ remote_hosts.go    │
│ iface.go     │ │          │ │ listen_ports.go    │
│              │ │          │ │ header.go          │
│              │ │          │ │ help.go / kill.go  │
│              │ │          │ │ format.go          │
│              │ │          │ │ styles.go          │
│              │ │          │ │ keys.go            │
└──────────────┘ └──────────┘ └────────────────────┘
```

## Package Structure

### `internal/platform/`

OS-specific socket enumeration and process mapping. Implements the `Platform` interface:

```go
type Platform interface {
    Collect() ([]MappedSocket, []model.InterfaceStats, error)
    Close() error
}
```

**Linux** (`linux.go`):
- **Tier 1**: Netlink SOCK_DIAG with INET_DIAG — queries kernel directly for TCP/UDP sockets with `tcp_info` byte counters (`bytes_acked`, `bytes_received`)
- **Tier 2**: `/proc/net/{tcp,tcp6,udp,udp6}` parsing + AF_PACKET raw capture for per-connection bandwidth
- Auto-detects which method to use at startup, with runtime failover

**Linux Process Mapping** (`linux_proc.go`):
- Scans `/proc/<pid>/fd/` for socket inodes
- Lazy-loads process info (name from `/proc/<pid>/comm`, cmdline from `/proc/<pid>/cmdline`)
- Builds inode → ProcessInfo map each poll cycle

**Linux AF_PACKET** (`linux_pcap.go`):
- `AF_PACKET, SOCK_DGRAM, ETH_P_ALL` raw socket
- Background goroutine parsing IPv4/IPv6 headers
- Tracks cumulative bytes per 5-tuple flow (proto, srcIP, dstIP, srcPort, dstPort)
- Prunes stale entries to prevent unbounded memory growth

**macOS** (`darwin.go`, `darwin_netstat.go`):
- `netstat -anb` for sockets with byte counters
- `lsof -i` for PID mapping
- `netstat -ibn` for interface stats

**Interface Detection** (`iface.go`):
- UDP dial to `8.8.8.8:53` to detect default outbound interface
- Fallback to first non-loopback UP interface

### `internal/collector/`

Aggregation and rate computation layer.

**Collector** (`collector.go`):
- Runs in its own goroutine, polling at configurable interval (default 1s)
- Per-socket tracking using `SocketKey` for delta computation
- Stale socket cleanup (30s timeout)
- Produces `model.Snapshot` on a buffered channel (size 1, non-blocking)
- Aggregates: per-process summaries, remote hosts, listen ports

**Bandwidth** (`bandwidth.go`):
- EMA (Exponential Moving Average) smoothing with alpha=0.3
- Applied to per-socket, per-process, and per-interface rates
- Safe counter-wrap handling (returns 0 delta)

**DNS** (`dns.go`):
- Async reverse DNS lookups via goroutines
- `sync.Map`-based deduplication
- 5-minute TTL cache, max 4096 entries
- 2s timeout per lookup
- Non-blocking: returns stale cache while refresh in progress

**History** (`history.go`):
- Ring buffer (circular buffer) for sparkline data
- Configurable size (16 for process, 60 for header)

### `internal/model/`

Data types shared across packages (`types.go`):
- `Socket`, `Connection`, `ProcessSummary`, `InterfaceStats`
- `RemoteHostSummary`, `ListenPortEntry`
- `Snapshot` — immutable point-in-time view of all data

### `internal/ui/`

Bubble Tea TUI layer following the Elm architecture (Model → Update → View).

**App** (`app.go`):
- Root `Model` struct with state management
- 4 view modes: ProcessTable, ProcessDetail, RemoteHosts, ListenPorts
- Handles all key/mouse events, view transitions

**Views**:
- `process_table.go` — main dashboard with sparklines, bandwidth bars, zebra striping
- `process_detail.go` — per-process connections with state badges, age, DNS
- `remote_hosts.go` — system-wide per-host bandwidth aggregation
- `listen_ports.go` — all listening ports with owning processes

**Components**:
- `header.go` — title, total rates, trend arrow, system sparkline, per-interface stats
- `help.go` — centered modal overlay with keybindings
- `kill.go` — signal selection overlay
- `format.go` — FormatRate, FormatBytes, FormatAge, Sparkline, BandwidthBar
- `styles.go` — Tokyo Night color palette, HSL interpolation for rate colors
- `keys.go` — key mapping abstraction

## Data Flow

```
1. Platform.Collect()
   → Raw sockets + process mapping + interface stats

2. Collector goroutine (every N seconds)
   → Compute deltas (bytes this interval)
   → EMA smooth rates
   → Aggregate per-process, per-host
   → Build Snapshot
   → Send on channel

3. UI Update()
   → Receive Snapshot via tea.Msg
   → Update view state
   → View() renders to terminal
```

## Concurrency Model

- **Collector goroutine**: single goroutine, polls platform at interval
- **DNS goroutines**: fire-and-forget lookups, sync.Map for thread safety
- **AF_PACKET goroutine**: background packet capture with RWMutex for flow map
- **UI goroutine**: single Bubble Tea event loop
- Communication: Go channels (Snapshot channel, stop channels)
