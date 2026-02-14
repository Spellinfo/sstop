//go:build darwin

package platform

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/googlesky/sstop/internal/model"
)

// netstatSocket holds a parsed socket from `netstat -anb`.
type netstatSocket struct {
	proto   model.Protocol
	srcIP   net.IP
	srcPort uint16
	dstIP   net.IP
	dstPort uint16
	state   model.SocketState
	bytesIn uint64
	bytesOut uint64
}

// lsofEntry holds a parsed entry from `lsof -i -n -P +c 0 -F pcnPtTn`.
type lsofEntry struct {
	pid     uint32
	command string
	proto   model.Protocol
	srcIP   net.IP
	srcPort uint16
	dstIP   net.IP
	dstPort uint16
	state   model.SocketState
}

// parseNetstatOutput parses the output of `netstat -anb -p tcp` or `netstat -anb -p udp`.
// macOS netstat -anb output looks like:
//
//	Active Internet connections (including servers)
//	Proto Recv-Q Send-Q  Local Address          Foreign Address        (state)      Bytes In  Bytes Out
//	tcp4       0      0  192.168.1.5.443        10.0.0.1.52341         ESTABLISHED  12345     67890
//	tcp4       0      0  *.80                   *.*                    LISTEN
//	tcp6       0      0  ::1.631                *.*                    LISTEN
func parseNetstatOutput(output string, proto model.Protocol) []netstatSocket {
	var sockets []netstatSocket
	scanner := bufio.NewScanner(strings.NewReader(output))

	// Skip until we find the header line
	headerFound := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Proto") || strings.Contains(line, "Local Address") {
			headerFound = true
			break
		}
	}
	if !headerFound {
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		s, err := parseNetstatLine(line, proto)
		if err != nil {
			continue
		}
		sockets = append(sockets, s)
	}

	return sockets
}

// parseNetstatLine parses a single line from macOS `netstat -anb` output.
func parseNetstatLine(line string, proto model.Protocol) (netstatSocket, error) {
	var s netstatSocket
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return s, fmt.Errorf("too few fields: %d", len(fields))
	}

	// fields[0] = Proto (tcp4, tcp6, udp4, udp6)
	protoField := fields[0]
	if !strings.HasPrefix(protoField, "tcp") && !strings.HasPrefix(protoField, "udp") {
		return s, fmt.Errorf("not a tcp/udp line")
	}
	isIPv6 := strings.HasSuffix(protoField, "6") || strings.HasSuffix(protoField, "46")

	// fields[1] = Recv-Q, fields[2] = Send-Q
	// fields[3] = Local Address
	// fields[4] = Foreign Address
	localAddr := fields[3]
	foreignAddr := fields[4]

	s.proto = proto

	var err error
	s.srcIP, s.srcPort, err = parseMacAddr(localAddr, isIPv6)
	if err != nil {
		return s, fmt.Errorf("parse local addr %q: %w", localAddr, err)
	}

	s.dstIP, s.dstPort, err = parseMacAddr(foreignAddr, isIPv6)
	if err != nil {
		return s, fmt.Errorf("parse foreign addr %q: %w", foreignAddr, err)
	}

	// Parse remaining fields: state (for TCP) and byte counters
	idx := 5
	if proto == model.ProtoTCP && idx < len(fields) {
		s.state = parseMacTCPState(fields[idx])
		idx++
	}

	// Bytes In / Bytes Out (only present with -b flag and for some connections)
	if idx < len(fields) {
		s.bytesIn, _ = strconv.ParseUint(fields[idx], 10, 64)
		idx++
	}
	if idx < len(fields) {
		s.bytesOut, _ = strconv.ParseUint(fields[idx], 10, 64)
	}

	return s, nil
}

// parseMacAddr parses a macOS netstat address like "192.168.1.5.443", "*.80", "::1.631", "*.*".
func parseMacAddr(addr string, isIPv6 bool) (net.IP, uint16, error) {
	if addr == "*.*" {
		return nil, 0, nil
	}

	// Handle wildcard addresses like "*.80"
	if strings.HasPrefix(addr, "*.") {
		portStr := addr[2:]
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil, 0, fmt.Errorf("parse wildcard port %q: %w", portStr, err)
		}
		return nil, uint16(port), nil
	}

	// For IPv6: addr looks like "::1.631" or "fe80::1%lo0.80"
	// The port is after the LAST dot.
	// For IPv4: addr looks like "192.168.1.5.443"
	// The port is after the LAST dot.

	lastDot := strings.LastIndex(addr, ".")
	if lastDot < 0 {
		// Could be just a port with wildcard: "*.0" handled above
		return nil, 0, fmt.Errorf("no dot in address: %q", addr)
	}

	ipPart := addr[:lastDot]
	portPart := addr[lastDot+1:]

	// Handle wildcard port
	if portPart == "*" {
		portPart = "0"
	}

	port, err := strconv.ParseUint(portPart, 10, 16)
	if err != nil {
		return nil, 0, fmt.Errorf("parse port %q: %w", portPart, err)
	}

	// Parse IP
	if ipPart == "*" {
		return nil, uint16(port), nil
	}

	ip := net.ParseIP(ipPart)
	if ip == nil {
		// For IPv4, the IP part looks like "192.168.1.5" which net.ParseIP handles.
		// For IPv6 with zone like "fe80::1%lo0", strip the zone.
		if pctIdx := strings.Index(ipPart, "%"); pctIdx >= 0 {
			ip = net.ParseIP(ipPart[:pctIdx])
		}
	}

	if ip == nil {
		return nil, 0, fmt.Errorf("cannot parse IP %q", ipPart)
	}

	return ip, uint16(port), nil
}

// parseMacTCPState maps macOS netstat TCP state strings to our SocketState.
func parseMacTCPState(s string) model.SocketState {
	switch strings.ToUpper(s) {
	case "ESTABLISHED":
		return model.StateEstablished
	case "SYN_SENT":
		return model.StateSynSent
	case "SYN_RECEIVED", "SYN_RCVD":
		return model.StateSynRecv
	case "FIN_WAIT_1":
		return model.StateFinWait1
	case "FIN_WAIT_2":
		return model.StateFinWait2
	case "TIME_WAIT":
		return model.StateTimeWait
	case "CLOSED":
		return model.StateClose
	case "CLOSE_WAIT":
		return model.StateCloseWait
	case "LAST_ACK":
		return model.StateLastAck
	case "LISTEN":
		return model.StateListen
	case "CLOSING":
		return model.StateClosing
	default:
		return model.StateUnknown
	}
}

// parseLsofOutput parses `lsof -i -n -P +c 0 -F pcnPtTn` field-format output.
// Output format:
//
//	p1234          (PID)
//	cfoo           (command name)
//	f3             (fd)
//	tIPv4          (type)
//	PTCP           (protocol)
//	TST=ESTABLISHED (TCP state)
//	n192.168.1.5:443->10.0.0.1:52341  (name: addr:port->addr:port)
func parseLsofOutput(output string) []lsofEntry {
	var entries []lsofEntry
	var current lsofEntry
	var hasCurrent bool

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 {
			continue
		}

		field := line[0]
		value := line[1:]

		switch field {
		case 'p':
			// New process — flush previous if complete
			pid, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				continue
			}
			current = lsofEntry{pid: uint32(pid)}
			hasCurrent = true

		case 'c':
			if hasCurrent {
				current.command = value
			}

		case 'P':
			if hasCurrent {
				switch strings.ToUpper(value) {
				case "TCP":
					current.proto = model.ProtoTCP
				case "UDP":
					current.proto = model.ProtoUDP
				}
			}

		case 'T':
			if hasCurrent && strings.HasPrefix(value, "ST=") {
				current.state = parseMacTCPState(value[3:])
			}

		case 'n':
			if !hasCurrent {
				continue
			}
			// Parse "addr:port->addr:port" or "addr:port" (listen) or "*:port"
			srcIP, srcPort, dstIP, dstPort, err := parseLsofName(value)
			if err != nil {
				continue
			}
			entry := current
			entry.srcIP = srcIP
			entry.srcPort = srcPort
			entry.dstIP = dstIP
			entry.dstPort = dstPort
			entries = append(entries, entry)
		}
	}

	return entries
}

// parseLsofName parses lsof name field: "ip:port->ip:port" or "ip:port" or "*:port".
func parseLsofName(name string) (srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, err error) {
	// Split on "->"
	parts := strings.SplitN(name, "->", 2)

	srcIP, srcPort, err = parseLsofAddr(parts[0])
	if err != nil {
		return nil, 0, nil, 0, fmt.Errorf("parse src %q: %w", parts[0], err)
	}

	if len(parts) == 2 {
		dstIP, dstPort, err = parseLsofAddr(parts[1])
		if err != nil {
			return nil, 0, nil, 0, fmt.Errorf("parse dst %q: %w", parts[1], err)
		}
	}

	return
}

// parseLsofAddr parses "ip:port", "[ip6]:port", or "*:port".
func parseLsofAddr(addr string) (net.IP, uint16, error) {
	if addr == "*:*" || addr == "" {
		return nil, 0, nil
	}

	// Handle IPv6 bracket notation: [::1]:631
	if strings.HasPrefix(addr, "[") {
		bracketEnd := strings.Index(addr, "]")
		if bracketEnd < 0 {
			return nil, 0, fmt.Errorf("unclosed bracket in %q", addr)
		}
		ipStr := addr[1:bracketEnd]
		rest := addr[bracketEnd+1:] // should be ":port"
		if !strings.HasPrefix(rest, ":") {
			return nil, 0, fmt.Errorf("expected :port after bracket in %q", addr)
		}
		port, err := strconv.ParseUint(rest[1:], 10, 16)
		if err != nil {
			return nil, 0, err
		}
		ip := net.ParseIP(ipStr)
		return ip, uint16(port), nil
	}

	// Handle wildcard: "*:80"
	if strings.HasPrefix(addr, "*:") {
		port, err := strconv.ParseUint(addr[2:], 10, 16)
		if err != nil {
			return nil, 0, err
		}
		return nil, uint16(port), nil
	}

	// IPv4 or IPv6 without brackets
	// Find last colon for port separator
	lastColon := strings.LastIndex(addr, ":")
	if lastColon < 0 {
		return nil, 0, fmt.Errorf("no port separator in %q", addr)
	}

	ipStr := addr[:lastColon]
	portStr := addr[lastColon+1:]

	if portStr == "*" {
		portStr = "0"
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, 0, fmt.Errorf("parse port %q: %w", portStr, err)
	}

	if ipStr == "*" {
		return nil, uint16(port), nil
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, 0, fmt.Errorf("invalid IP %q", ipStr)
	}

	return ip, uint16(port), nil
}

// parseNetstatInterfaces parses `netstat -ibn` output for macOS interface stats.
// Output looks like:
//
//	Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
//	en0   1500  <Link#4>      aa:bb:cc:dd:ee:ff  12345     0    1234567    67890     0    7654321     0
func parseNetstatInterfaces(output string) []model.InterfaceStats {
	var result []model.InterfaceStats
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(output))
	// Skip header
	if !scanner.Scan() {
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		name := fields[0]

		// Only process link-layer lines (contain "<Link#")
		if !strings.Contains(fields[2], "<Link#") {
			continue
		}

		// Skip loopback
		if name == "lo0" {
			continue
		}

		// Deduplicate
		if seen[name] {
			continue
		}
		seen[name] = true

		ibytes, _ := strconv.ParseUint(fields[6], 10, 64)
		obytes, _ := strconv.ParseUint(fields[9], 10, 64)

		result = append(result, model.InterfaceStats{
			Name:      name,
			BytesRecv: ibytes,
			BytesSent: obytes,
		})
	}

	return result
}
