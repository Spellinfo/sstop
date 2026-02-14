//go:build linux

package platform

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/googlesky/sstop/internal/model"
	"github.com/mdlayher/netlink"
)

const (
	// Netlink constants for INET_DIAG
	sockDiagByFamily = 20 // SOCK_DIAG_BY_FAMILY
	inetDiagInfo     = 2  // INET_DIAG_INFO attribute

	// Address families
	afINET  = 2  // AF_INET
	afINET6 = 10 // AF_INET6

	// Socket protocols
	ipprotoTCP = 6  // IPPROTO_TCP
	ipprotoUDP = 17 // IPPROTO_UDP

	// All TCP states bitmask (for querying all states)
	allTCPStates = 0xFFF
)

// inetDiagReqV2 is the wire format for sock_diag request (56 bytes).
type inetDiagReqV2 struct {
	Family   uint8
	Protocol uint8
	Ext      uint8
	Pad      uint8
	States   uint32
	ID       inetDiagSockID
}

// inetDiagSockID identifies a socket (48 bytes).
type inetDiagSockID struct {
	SPort  [2]byte  // source port (network byte order)
	DPort  [2]byte  // dest port (network byte order)
	Src    [16]byte // source address
	Dst    [16]byte // dest address
	If     uint32
	Cookie [2]uint32
}

// inetDiagMsg is the response header (72 bytes).
type inetDiagMsg struct {
	Family  uint8
	State   uint8
	Timer   uint8
	Retrans uint8
	ID      inetDiagSockID
	Expires uint32
	RQueue  uint32
	WQueue  uint32
	UID     uint32
	Inode   uint32
}

// LinuxPlatform collects network data using netlink and /proc.
type LinuxPlatform struct {
	// conn is the netlink SOCK_DIAG connection. nil if netlink is unavailable
	// (e.g. inet_diag / tcp_diag kernel modules not loaded).
	conn *netlink.Conn

	// useProc is true when we must fall back to parsing /proc/net/{tcp,udp}
	// instead of using netlink INET_DIAG. This happens when the inet_diag
	// kernel module is not available (CONFIG_INET_DIAG=m but module files
	// missing for the running kernel).
	useProc bool

	// pcap tracks per-connection bytes via AF_PACKET when inet_diag is unavailable.
	// nil when using netlink (not needed) or when AF_PACKET is not available.
	pcap *packetCounter
}

// NewPlatform creates a new Linux platform collector.
// It attempts netlink SOCK_DIAG first, then probes whether the kernel actually
// supports INET_DIAG queries. If the inet_diag module is not available, it
// falls back to /proc/net/{tcp,udp,tcp6,udp6} parsing transparently.
func NewPlatform() (Platform, error) {
	p := &LinuxPlatform{}

	// NETLINK_SOCK_DIAG = 4
	conn, err := netlink.Dial(4, nil)
	if err != nil {
		// Cannot even open the netlink socket -- fall back to /proc.
		log.Printf("sstop: netlink dial failed, using /proc + AF_PACKET fallback: %v", err)
		p.useProc = true
		p.pcap = newPacketCounter()
		return p, nil
	}

	// Probe: send a minimal TCP IPv4 query to see if the kernel handles it.
	// The kernel returns ENOENT when inet_diag/tcp_diag modules are missing.
	if probeErr := probeNetlinkDiag(conn); probeErr != nil {
		// Try to auto-load the inet_diag/tcp_diag kernel modules.
		// These are often compiled as modules (CONFIG_INET_DIAG=m) and not
		// loaded by default. Loading tcp_diag pulls in inet_diag as a dependency.
		loaded := false
		for _, mod := range []string{"tcp_diag", "udp_diag"} {
			if err := exec.Command("modprobe", mod).Run(); err == nil {
				loaded = true
			}
		}
		if loaded {
			// Re-probe after loading modules
			if probeNetlinkDiag(conn) == nil {
				log.Printf("sstop: auto-loaded inet_diag kernel modules")
				p.conn = conn
				return p, nil
			}
		}

		conn.Close()
		log.Printf("sstop: netlink INET_DIAG unavailable, using /proc + AF_PACKET fallback: %v", probeErr)
		p.useProc = true
		p.pcap = newPacketCounter()
		return p, nil
	}

	p.conn = conn
	return p, nil
}

// probeNetlinkDiag sends a minimal SOCK_DIAG_BY_FAMILY request for TCP/IPv4
// to verify the kernel can actually process INET_DIAG queries. Returns nil on
// success. Returns an error if the kernel rejects the request (typically ENOENT
// when inet_diag/tcp_diag modules are not loaded).
func probeNetlinkDiag(conn *netlink.Conn) error {
	req := inetDiagReqV2{
		Family:   afINET,
		Protocol: ipprotoTCP,
		States:   allTCPStates,
	}
	reqBytes := (*[unsafe.Sizeof(req)]byte)(unsafe.Pointer(&req))[:]

	msg := netlink.Message{
		Header: netlink.Header{
			Type:  sockDiagByFamily,
			Flags: netlink.Request | netlink.Dump,
		},
		Data: reqBytes,
	}

	_, err := conn.Execute(msg)
	return err
}

// isNetlinkModuleError returns true if the error indicates that the kernel
// module for sock_diag is not available (ENOENT = "no such file or directory").
func isNetlinkModuleError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ENOENT
	}
	// Also check the *netlink.OpError wrapper
	var opErr *netlink.OpError
	if errors.As(err, &opErr) {
		return errors.Is(opErr.Err, syscall.ENOENT)
	}
	return false
}

func (p *LinuxPlatform) Close() error {
	if p.pcap != nil {
		p.pcap.close()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

func (p *LinuxPlatform) Collect() ([]MappedSocket, []model.InterfaceStats, error) {
	// 1. Get all sockets via netlink or /proc fallback
	var sockets []model.Socket
	var err error
	if p.useProc {
		sockets, err = querySocketsFromProc()
	} else {
		sockets, err = p.queryAllSockets()
		// If netlink fails at runtime (e.g. module unloaded), try /proc fallback
		if err != nil && isNetlinkModuleError(err) {
			log.Printf("sstop: netlink query failed at runtime, falling back to /proc + AF_PACKET: %v", err)
			p.useProc = true
			if p.conn != nil {
				p.conn.Close()
				p.conn = nil
			}
			if p.pcap == nil {
				p.pcap = newPacketCounter()
			}
			sockets, err = querySocketsFromProc()
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query sockets: %w", err)
	}

	// 2. Scan /proc for inode->PID mapping
	inodeMap, err := ScanProcesses()
	if err != nil {
		return nil, nil, fmt.Errorf("scan processes: %w", err)
	}

	// 3. Map sockets to processes and fill byte counters from packet capture
	var mapped []MappedSocket
	var activeFlows map[flowKey]bool
	if p.pcap != nil {
		activeFlows = make(map[flowKey]bool)
	}

	for i := range sockets {
		ms := MappedSocket{Socket: sockets[i]}
		if info, ok := inodeMap[sockets[i].Inode]; ok {
			ms.PID = info.PID
			ms.ProcessName = info.Name
			ms.Cmdline = info.Cmdline
		}

		// Fill byte counters from packet capture when inet_diag is unavailable
		if p.pcap != nil && ms.DstIP != nil && !ms.DstIP.IsUnspecified() {
			var proto uint8
			if ms.Proto == model.ProtoTCP {
				proto = 6
			} else {
				proto = 17
			}
			sent, recv := p.pcap.getBytes(proto, ms.SrcIP, ms.SrcPort, ms.DstIP, ms.DstPort)
			ms.BytesSent = sent
			ms.BytesRecv = recv

			// Track active flows for pruning
			lIP := ipTo16(ms.SrcIP)
			rIP := ipTo16(ms.DstIP)
			activeFlows[flowKey{proto: proto, srcIP: lIP, dstIP: rIP, srcPort: ms.SrcPort, dstPort: ms.DstPort}] = true
			activeFlows[flowKey{proto: proto, srcIP: rIP, dstIP: lIP, srcPort: ms.DstPort, dstPort: ms.SrcPort}] = true
		}

		mapped = append(mapped, ms)
	}

	// Prune stale flow entries periodically
	if p.pcap != nil && activeFlows != nil {
		p.pcap.prune(activeFlows)
	}

	// 4. Get interface stats
	ifaces, err := ParseNetDev()
	if err != nil {
		// Non-fatal; return sockets without interface stats
		ifaces = nil
	}

	return mapped, ifaces, nil
}

func (p *LinuxPlatform) queryAllSockets() ([]model.Socket, error) {
	var all []model.Socket

	// Query TCP (IPv4 + IPv6)
	for _, af := range []uint8{afINET, afINET6} {
		socks, err := p.querySockets(af, ipprotoTCP, model.ProtoTCP)
		if err != nil {
			return nil, fmt.Errorf("query TCP af=%d: %w", af, err)
		}
		all = append(all, socks...)
	}

	// Query UDP (IPv4 + IPv6)
	for _, af := range []uint8{afINET, afINET6} {
		socks, err := p.querySockets(af, ipprotoUDP, model.ProtoUDP)
		if err != nil {
			// UDP query may fail on some kernels, non-fatal
			continue
		}
		all = append(all, socks...)
	}

	return all, nil
}

func (p *LinuxPlatform) querySockets(family, protocol uint8, proto model.Protocol) ([]model.Socket, error) {
	req := inetDiagReqV2{
		Family:   family,
		Protocol: protocol,
		States:   allTCPStates,
	}
	if protocol == ipprotoTCP {
		req.Ext = 1 << (inetDiagInfo - 1) // request TCP_INFO
	}

	reqBytes := (*[unsafe.Sizeof(req)]byte)(unsafe.Pointer(&req))[:]

	msg := netlink.Message{
		Header: netlink.Header{
			Type:  sockDiagByFamily,
			Flags: netlink.Request | netlink.Dump,
		},
		Data: reqBytes,
	}

	msgs, err := p.conn.Execute(msg)
	if err != nil {
		return nil, err
	}

	var sockets []model.Socket
	for _, m := range msgs {
		s, err := parseDiagMsg(m.Data, family, proto)
		if err != nil {
			continue
		}
		sockets = append(sockets, s)
	}

	return sockets, nil
}

func parseDiagMsg(data []byte, family uint8, proto model.Protocol) (model.Socket, error) {
	var s model.Socket

	if len(data) < int(unsafe.Sizeof(inetDiagMsg{})) {
		return s, fmt.Errorf("message too short: %d", len(data))
	}

	msg := (*inetDiagMsg)(unsafe.Pointer(&data[0]))

	s.Proto = proto
	s.State = mapTCPState(msg.State)
	s.Inode = uint64(msg.Inode)

	sport := binary.BigEndian.Uint16(msg.ID.SPort[:])
	dport := binary.BigEndian.Uint16(msg.ID.DPort[:])
	s.SrcPort = sport
	s.DstPort = dport

	if family == afINET {
		s.SrcIP = net.IP(msg.ID.Src[:4]).To4()
		s.DstIP = net.IP(msg.ID.Dst[:4]).To4()
	} else {
		s.SrcIP = make(net.IP, 16)
		copy(s.SrcIP, msg.ID.Src[:])
		s.DstIP = make(net.IP, 16)
		copy(s.DstIP, msg.ID.Dst[:])
	}

	// Parse TCP_INFO from netlink attributes for byte counters
	if proto == model.ProtoTCP {
		parseTCPInfoFromAttrs(data[unsafe.Sizeof(inetDiagMsg{}):], &s)
	}

	return s, nil
}

func parseTCPInfoFromAttrs(data []byte, s *model.Socket) {
	// Parse netlink attributes to find INET_DIAG_INFO
	attrs, err := netlink.UnmarshalAttributes(data)
	if err != nil {
		return
	}

	for _, attr := range attrs {
		if int(attr.Type) == inetDiagInfo {
			// This is struct tcp_info
			// bytes_acked at offset 120 (uint64)
			// bytes_received at offset 128 (uint64)
			tcpInfoData := attr.Data
			if len(tcpInfoData) >= 136 {
				s.BytesSent = binary.LittleEndian.Uint64(tcpInfoData[120:128])
				s.BytesRecv = binary.LittleEndian.Uint64(tcpInfoData[128:136])
			}
			break
		}
	}
}

// mapTCPState maps kernel TCP state values to our SocketState.
func mapTCPState(kernelState uint8) model.SocketState {
	// Kernel TCP states match our enum values 1:1 for 1-11
	if kernelState >= 1 && kernelState <= 11 {
		return model.SocketState(kernelState)
	}
	return model.StateUnknown
}
