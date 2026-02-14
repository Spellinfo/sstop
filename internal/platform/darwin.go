//go:build darwin

package platform

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/googlesky/sstop/internal/model"
)

// DarwinPlatform collects network data using netstat and lsof on macOS.
type DarwinPlatform struct{}

// NewPlatform creates a new macOS platform collector.
func NewPlatform() (Platform, error) {
	return &DarwinPlatform{}, nil
}

func (p *DarwinPlatform) Close() error {
	return nil
}

func (p *DarwinPlatform) Collect() ([]MappedSocket, []model.InterfaceStats, error) {
	// 1. Run netstat for TCP and UDP sockets with byte counters
	tcpSockets, err := p.runNetstat("tcp")
	if err != nil {
		return nil, nil, fmt.Errorf("netstat tcp: %w", err)
	}
	udpSockets, err := p.runNetstat("udp")
	if err != nil {
		// UDP netstat failure is non-fatal
		udpSockets = nil
	}

	allNetstat := append(tcpSockets, udpSockets...)

	// 2. Run lsof to get PID→socket mapping
	lsofEntries, err := p.runLsof()
	if err != nil {
		// lsof failure is non-fatal; we just won't have PID info
		lsofEntries = nil
	}

	// 3. Build lookup from lsof entries by (src:port, dst:port)
	type addrKey struct {
		srcAddr string
		dstAddr string
		proto   model.Protocol
	}
	lsofMap := make(map[addrKey]*lsofEntry)
	for i := range lsofEntries {
		e := &lsofEntries[i]
		key := addrKey{
			srcAddr: normalizeAddr(e.srcIP, e.srcPort),
			dstAddr: normalizeAddr(e.dstIP, e.dstPort),
			proto:   e.proto,
		}
		lsofMap[key] = e
	}

	// 4. Match netstat sockets with lsof entries
	var mapped []MappedSocket
	for _, ns := range allNetstat {
		ms := MappedSocket{
			Socket: model.Socket{
				Proto:     ns.proto,
				SrcIP:     ns.srcIP,
				SrcPort:   ns.srcPort,
				DstIP:     ns.dstIP,
				DstPort:   ns.dstPort,
				State:     ns.state,
				BytesSent: ns.bytesOut,
				BytesRecv: ns.bytesIn,
			},
		}

		key := addrKey{
			srcAddr: normalizeAddr(ns.srcIP, ns.srcPort),
			dstAddr: normalizeAddr(ns.dstIP, ns.dstPort),
			proto:   ns.proto,
		}
		if e, ok := lsofMap[key]; ok {
			ms.PID = e.pid
			ms.ProcessName = e.command
		}

		mapped = append(mapped, ms)
	}

	// 5. Get interface stats
	ifaces, err := p.runNetstatInterfaces()
	if err != nil {
		ifaces = nil
	}

	return mapped, ifaces, nil
}

const cmdTimeout = 5 * time.Second

func (p *DarwinPlatform) runNetstat(proto string) ([]netstatSocket, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "netstat", "-anb", "-p", proto).Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -anb -p %s: %w", proto, err)
	}

	var modelProto model.Protocol
	switch proto {
	case "tcp":
		modelProto = model.ProtoTCP
	case "udp":
		modelProto = model.ProtoUDP
	}

	return parseNetstatOutput(string(out), modelProto), nil
}

func (p *DarwinPlatform) runLsof() ([]lsofEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-i", "-n", "-P", "+c", "0", "-F", "pcnPtTn").Output()
	if err != nil {
		return nil, fmt.Errorf("exec lsof: %w", err)
	}
	return parseLsofOutput(string(out)), nil
}

func (p *DarwinPlatform) runNetstatInterfaces() ([]model.InterfaceStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "netstat", "-ibn").Output()
	if err != nil {
		return nil, fmt.Errorf("exec netstat -ibn: %w", err)
	}
	return parseNetstatInterfaces(string(out)), nil
}

// normalizeAddr formats ip:port for matching. Handles nil IPs as wildcard.
func normalizeAddr(ip net.IP, port uint16) string {
	if ip == nil || ip.IsUnspecified() {
		return fmt.Sprintf("*:%d", port)
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%s:%d", ip4, port)
	}
	return fmt.Sprintf("[%s]:%d", ip, port)
}

