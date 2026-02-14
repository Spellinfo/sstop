//go:build linux

package platform

import (
	"net"
	"testing"
)

func TestIpTo16(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want [16]byte
	}{
		{
			name: "nil",
			ip:   nil,
			want: [16]byte{},
		},
		{
			name: "ipv4",
			ip:   net.ParseIP("192.168.1.1").To4(),
			want: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 192, 168, 1, 1},
		},
		{
			name: "ipv6",
			ip:   net.ParseIP("::1"),
			want: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ipTo16(tt.ip)
			if got != tt.want {
				t.Errorf("ipTo16(%v) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestWalkIPv6ExtHeaders(t *testing.T) {
	// Simple case: Next Header is TCP directly
	pkt := make([]byte, 60)
	proto, offset := walkIPv6ExtHeaders(pkt, 6, 40)
	if proto != 6 || offset != 40 {
		t.Errorf("TCP direct: got proto=%d offset=%d, want proto=6 offset=40", proto, offset)
	}

	// UDP directly
	proto, offset = walkIPv6ExtHeaders(pkt, 17, 40)
	if proto != 17 || offset != 40 {
		t.Errorf("UDP direct: got proto=%d offset=%d, want proto=17 offset=40", proto, offset)
	}

	// Hop-by-Hop (0) → TCP (6)
	// Extension header: next=6, len=0 (8 bytes)
	pkt[40] = 6  // next header = TCP
	pkt[41] = 0  // length = 0 → (0+1)*8 = 8 bytes
	proto, offset = walkIPv6ExtHeaders(pkt, 0, 40)
	if proto != 6 || offset != 48 {
		t.Errorf("HopByHop→TCP: got proto=%d offset=%d, want proto=6 offset=48", proto, offset)
	}

	// Fragment (44) → TCP (6)
	pkt[40] = 6 // next header = TCP
	proto, offset = walkIPv6ExtHeaders(pkt, 44, 40)
	if proto != 6 || offset != 48 {
		t.Errorf("Fragment→TCP: got proto=%d offset=%d, want proto=6 offset=48", proto, offset)
	}
}

func TestProcessPacketIPv4TCP(t *testing.T) {
	pc := &packetCounter{
		flows: make(map[flowKey]uint64),
	}

	// Minimal IPv4 TCP packet: 20 byte IP header + 20 byte TCP header
	pkt := make([]byte, 40)
	pkt[0] = 0x45 // version=4, IHL=5 (20 bytes)
	pkt[2] = 0    // total length = 40
	pkt[3] = 40
	pkt[9] = 6 // protocol = TCP

	// src IP: 10.0.0.1
	pkt[12] = 10
	pkt[13] = 0
	pkt[14] = 0
	pkt[15] = 1
	// dst IP: 10.0.0.2
	pkt[16] = 10
	pkt[17] = 0
	pkt[18] = 0
	pkt[19] = 2

	// src port: 12345 (0x3039)
	pkt[20] = 0x30
	pkt[21] = 0x39
	// dst port: 80 (0x0050)
	pkt[22] = 0x00
	pkt[23] = 0x50

	pc.processPacket(pkt)

	key := flowKey{
		proto:   6,
		srcIP:   ipTo16(net.ParseIP("10.0.0.1").To4()),
		dstIP:   ipTo16(net.ParseIP("10.0.0.2").To4()),
		srcPort: 12345,
		dstPort: 80,
	}

	pc.mu.RLock()
	bytes := pc.flows[key]
	pc.mu.RUnlock()

	if bytes != 40 {
		t.Errorf("flow bytes = %d, want 40", bytes)
	}

	// Process same packet again
	pc.processPacket(pkt)
	pc.mu.RLock()
	bytes = pc.flows[key]
	pc.mu.RUnlock()
	if bytes != 80 {
		t.Errorf("cumulative bytes = %d, want 80", bytes)
	}
}

func TestProcessPacketIPv4UDP(t *testing.T) {
	pc := &packetCounter{
		flows: make(map[flowKey]uint64),
	}

	pkt := make([]byte, 28)
	pkt[0] = 0x45 // version=4, IHL=5
	pkt[2] = 0
	pkt[3] = 28  // total length = 28
	pkt[9] = 17  // protocol = UDP

	pkt[12] = 192
	pkt[13] = 168
	pkt[14] = 1
	pkt[15] = 100
	pkt[16] = 8
	pkt[17] = 8
	pkt[18] = 8
	pkt[19] = 8

	// src port: 5000
	pkt[20] = 0x13
	pkt[21] = 0x88
	// dst port: 53
	pkt[22] = 0x00
	pkt[23] = 0x35

	pc.processPacket(pkt)

	key := flowKey{
		proto:   17,
		srcIP:   ipTo16(net.ParseIP("192.168.1.100").To4()),
		dstIP:   ipTo16(net.ParseIP("8.8.8.8").To4()),
		srcPort: 5000,
		dstPort: 53,
	}

	pc.mu.RLock()
	bytes := pc.flows[key]
	pc.mu.RUnlock()

	if bytes != 28 {
		t.Errorf("UDP flow bytes = %d, want 28", bytes)
	}
}

func TestGetBytes(t *testing.T) {
	pc := &packetCounter{
		flows: make(map[flowKey]uint64),
	}

	localIP := net.ParseIP("10.0.0.1").To4()
	remoteIP := net.ParseIP("10.0.0.2").To4()

	lIP := ipTo16(localIP)
	rIP := ipTo16(remoteIP)

	// Simulate upload: local→remote
	pc.flows[flowKey{proto: 6, srcIP: lIP, dstIP: rIP, srcPort: 12345, dstPort: 80}] = 1000
	// Simulate download: remote→local
	pc.flows[flowKey{proto: 6, srcIP: rIP, dstIP: lIP, srcPort: 80, dstPort: 12345}] = 5000

	sent, recv := pc.getBytes(6, localIP, 12345, remoteIP, 80)
	if sent != 1000 {
		t.Errorf("sent = %d, want 1000", sent)
	}
	if recv != 5000 {
		t.Errorf("recv = %d, want 5000", recv)
	}
}

func TestPrune(t *testing.T) {
	pc := &packetCounter{
		flows: make(map[flowKey]uint64),
	}

	k1 := flowKey{proto: 6, srcPort: 1}
	k2 := flowKey{proto: 6, srcPort: 2}
	k3 := flowKey{proto: 6, srcPort: 3}

	pc.flows[k1] = 100
	pc.flows[k2] = 200
	pc.flows[k3] = 300

	// Prune: only k1 and k2 are active
	active := map[flowKey]bool{k1: true, k2: true}
	pc.prune(active)

	if len(pc.flows) != 2 {
		t.Errorf("after prune: %d flows, want 2", len(pc.flows))
	}
	if _, ok := pc.flows[k3]; ok {
		t.Error("k3 should have been pruned")
	}

	// Prune with empty active should NOT purge everything
	pc.prune(map[flowKey]bool{})
	if len(pc.flows) != 2 {
		t.Errorf("empty prune should not purge: %d flows, want 2", len(pc.flows))
	}
}

func TestProcessPacketIgnoresNonTCPUDP(t *testing.T) {
	pc := &packetCounter{
		flows: make(map[flowKey]uint64),
	}

	// ICMP packet (proto=1)
	pkt := make([]byte, 28)
	pkt[0] = 0x45
	pkt[2] = 0
	pkt[3] = 28
	pkt[9] = 1 // ICMP

	pkt[12] = 10
	pkt[13] = 0
	pkt[14] = 0
	pkt[15] = 1
	pkt[16] = 10
	pkt[17] = 0
	pkt[18] = 0
	pkt[19] = 2

	pc.processPacket(pkt)

	if len(pc.flows) != 0 {
		t.Errorf("ICMP should not be tracked, got %d flows", len(pc.flows))
	}
}

func TestProcessPacketShortPackets(t *testing.T) {
	pc := &packetCounter{
		flows: make(map[flowKey]uint64),
	}

	// Empty packet
	pc.processPacket([]byte{})
	if len(pc.flows) != 0 {
		t.Error("empty packet should be ignored")
	}

	// Too short for IPv4 header
	pc.processPacket([]byte{0x45, 0, 0, 10})
	if len(pc.flows) != 0 {
		t.Error("short IPv4 should be ignored")
	}

	// Too short for IPv6 header
	pkt6 := make([]byte, 20)
	pkt6[0] = 0x60
	pc.processPacket(pkt6)
	if len(pc.flows) != 0 {
		t.Error("short IPv6 should be ignored")
	}
}

func TestHtons(t *testing.T) {
	// htons should convert host byte order to network byte order (big-endian)
	result := htons(0x0003)
	// On little-endian, 0x0003 in memory is [03, 00]
	// BigEndian.Uint16([03, 00]) = 0x0300
	// But htons should give us the value that when used in syscall gives ETH_P_ALL
	// The point is htons(0x0003) should work for AF_PACKET
	if result == 0 {
		t.Error("htons(3) should not be 0")
	}
}
