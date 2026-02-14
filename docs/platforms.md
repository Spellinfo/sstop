# Platform Support

## Linux

### Bandwidth Tracking

sstop uses a tiered fallback strategy on Linux:

#### Tier 1: Netlink SOCK_DIAG (preferred)

Uses the `NETLINK_SOCK_DIAG` socket to query the kernel for all TCP and UDP sockets. Requests the `INET_DIAG_INFO` attribute which contains `struct tcp_info` with per-connection byte counters (`tcpi_bytes_acked` and `tcpi_bytes_received`).

**Requirements**: `inet_diag` and `tcp_diag` kernel modules.

On startup, sstop probes whether the kernel supports INET_DIAG by sending a test query. If it fails, it automatically attempts to load the required modules via `modprobe tcp_diag udp_diag`.

#### Tier 2: /proc/net + AF_PACKET (fallback)

When the `inet_diag` module is unavailable (common on minimal or custom kernels like CachyOS), sstop falls back to:

1. **Socket enumeration**: Parses `/proc/net/{tcp,tcp6,udp,udp6}` for socket information
2. **Per-connection bandwidth**: Opens an `AF_PACKET` raw socket (`SOCK_DGRAM, ETH_P_ALL`) to capture all network packets and track per-flow byte counters

The AF_PACKET fallback:
- Captures all IP packets (IPv4 and IPv6)
- Parses transport headers (TCP/UDP) to extract 5-tuple flow keys
- Handles IPv6 extension header chains
- Uses 4MB receive buffer for high-throughput capture
- Periodically prunes stale flow entries

### Process Mapping

Scans `/proc/<pid>/fd/` for all processes to find socket symlinks matching `socket:[<inode>]`. Process metadata comes from `/proc/<pid>/comm` (name) and `/proc/<pid>/cmdline` (full command line).

### Interface Stats

Parsed from `/proc/net/dev`. Loopback interface (`lo`) is excluded.

### Permissions

```bash
# Option 1: Run as root
sudo sstop

# Option 2: Grant capabilities (for netlink + AF_PACKET)
sudo setcap cap_net_raw+ep ./sstop

# Option 3: Minimal — only /proc parsing (no per-connection bandwidth)
# No special permissions needed, but bandwidth bars/sparklines won't work
```

### Kernel Module Loading

If you see in the logs:
```
sstop: netlink INET_DIAG unavailable, using /proc + AF_PACKET fallback
```

You can load the modules manually:
```bash
sudo modprobe tcp_diag
sudo modprobe udp_diag
```

Or make them load at boot:
```bash
echo "tcp_diag" | sudo tee /etc/modules-load.d/tcp_diag.conf
echo "udp_diag" | sudo tee /etc/modules-load.d/udp_diag.conf
```

## macOS

### Bandwidth Tracking

Uses external commands (all with 5-second timeouts):

1. **`netstat -anb -p tcp`** / **`netstat -anb -p udp`** — enumerates sockets with byte counters (Bytes In/Bytes Out columns)
2. **`lsof -i -n -P +c 0 -F pcnPtTn`** — maps sockets to PIDs and process names via field-format output

### Interface Stats

Parsed from `netstat -ibn`, extracting link-layer lines (skipping `lo0`).

### Permissions

```bash
# Requires root for netstat byte counters and lsof process mapping
sudo sstop
```

## Cross-Platform Features

### Interface Auto-Detection

sstop automatically detects the default outbound network interface by attempting a UDP dial to `8.8.8.8:53` and identifying which local address is used. Falls back to the first non-loopback UP interface with a valid address.

### DNS Resolution

Reverse DNS lookups work identically on all platforms using Go's `net.LookupAddr()`. Results are cached for 5 minutes with a max cache size of 4096 entries.
