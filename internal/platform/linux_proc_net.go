//go:build linux

package platform

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/googlesky/sstop/internal/model"
)

// /proc/net/{tcp,tcp6,udp,udp6} column layout (after the header line):
//
//   sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
//   0:  0100007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12345 ...
//
// Fields are whitespace-separated. The address format is hex IP:hex port.

// procNetFile describes a /proc/net file to parse.
type procNetFile struct {
	path   string
	family uint8
	proto  model.Protocol
}

// querySocketsFromProc parses /proc/net/{tcp,tcp6,udp,udp6} to enumerate
// all sockets. This is the fallback when netlink INET_DIAG is unavailable
// (e.g. inet_diag kernel module not loaded).
//
// Limitations compared to netlink:
//   - No TCP_INFO (bytes_acked/bytes_received), so BytesSent/BytesRecv will
//     remain zero. Bandwidth tracking must rely on interface-level counters.
//   - Slightly higher overhead from text parsing vs binary netlink messages.
func querySocketsFromProc() ([]model.Socket, error) {
	files := []procNetFile{
		{"/proc/net/tcp", afINET, model.ProtoTCP},
		{"/proc/net/tcp6", afINET6, model.ProtoTCP},
		{"/proc/net/udp", afINET, model.ProtoUDP},
		{"/proc/net/udp6", afINET6, model.ProtoUDP},
	}

	var all []model.Socket
	for _, pf := range files {
		socks, err := parseProcNetFile(pf.path, pf.family, pf.proto)
		if err != nil {
			// UDP files might not exist on some configs; TCP is required.
			if pf.proto == model.ProtoUDP {
				continue
			}
			return nil, fmt.Errorf("parse %s: %w", pf.path, err)
		}
		all = append(all, socks...)
	}

	return all, nil
}

// parseProcNetFile reads a single /proc/net/{tcp,tcp6,udp,udp6} file.
func parseProcNetFile(path string, family uint8, proto model.Protocol) ([]model.Socket, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sockets []model.Socket
	scanner := bufio.NewScanner(f)

	// Skip header line
	if !scanner.Scan() {
		return nil, scanner.Err()
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		s, err := parseProcNetLine(line, family, proto)
		if err != nil {
			// Skip unparseable lines rather than failing entirely.
			continue
		}
		sockets = append(sockets, s)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return sockets, nil
}

// parseProcNetLine parses a single line from /proc/net/{tcp,tcp6,udp,udp6}.
func parseProcNetLine(line string, family uint8, proto model.Protocol) (model.Socket, error) {
	var s model.Socket

	fields := strings.Fields(line)
	if len(fields) < 10 {
		return s, fmt.Errorf("too few fields: %d", len(fields))
	}

	// fields[1] = local_address  (hex_ip:hex_port)
	// fields[2] = rem_address    (hex_ip:hex_port)
	// fields[3] = state          (hex)
	// fields[7] = uid
	// fields[9] = inode

	srcIP, srcPort, err := parseProcAddr(fields[1], family)
	if err != nil {
		return s, fmt.Errorf("parse local addr: %w", err)
	}

	dstIP, dstPort, err := parseProcAddr(fields[2], family)
	if err != nil {
		return s, fmt.Errorf("parse remote addr: %w", err)
	}

	state, err := strconv.ParseUint(fields[3], 16, 8)
	if err != nil {
		return s, fmt.Errorf("parse state: %w", err)
	}

	inode, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil {
		return s, fmt.Errorf("parse inode: %w", err)
	}

	s.Proto = proto
	s.SrcIP = srcIP
	s.SrcPort = srcPort
	s.DstIP = dstIP
	s.DstPort = dstPort
	s.State = mapTCPState(uint8(state))
	s.Inode = inode
	// BytesSent and BytesRecv remain 0 -- /proc/net/tcp does not expose
	// per-socket byte counters (those come from TCP_INFO via netlink).

	return s, nil
}

// parseProcAddr parses a /proc/net address in the form "HEXIP:HEXPORT".
// For IPv4, HEXIP is 8 hex chars in little-endian uint32 format.
// For IPv6, HEXIP is 32 hex chars in groups of 4 little-endian uint32s.
func parseProcAddr(s string, family uint8) (net.IP, uint16, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil, 0, fmt.Errorf("invalid address format: %q", s)
	}

	portVal, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid port: %w", err)
	}

	ipHex := parts[0]
	ipBytes, err := hex.DecodeString(ipHex)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid IP hex: %w", err)
	}

	var ip net.IP

	if family == afINET {
		// IPv4: 4 bytes stored as little-endian uint32
		if len(ipBytes) != 4 {
			return nil, 0, fmt.Errorf("expected 4 IP bytes for AF_INET, got %d", len(ipBytes))
		}
		// Reverse byte order (little-endian to network byte order)
		ip = net.IPv4(ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0]).To4()
	} else {
		// IPv6: 16 bytes stored as 4 little-endian uint32 groups
		if len(ipBytes) != 16 {
			return nil, 0, fmt.Errorf("expected 16 IP bytes for AF_INET6, got %d", len(ipBytes))
		}
		ip = make(net.IP, 16)
		// Each 4-byte group is in little-endian order
		for i := 0; i < 4; i++ {
			ip[i*4+0] = ipBytes[i*4+3]
			ip[i*4+1] = ipBytes[i*4+2]
			ip[i*4+2] = ipBytes[i*4+1]
			ip[i*4+3] = ipBytes[i*4+0]
		}
	}

	return ip, uint16(portVal), nil
}
