//go:build linux

package platform

import (
	"encoding/binary"
	"log"
	"net"
	"sync"
	"syscall"
	"unsafe"
)

// packetCounter uses AF_PACKET raw sockets to track per-flow byte counters.
// This is the fallback for systems without the inet_diag kernel module,
// where /proc/net/tcp has no per-socket byte counters.
type packetCounter struct {
	fd       int
	mu       sync.RWMutex
	flows    map[flowKey]uint64 // 5-tuple → cumulative bytes
	stopCh   chan struct{}
	done     chan struct{}
	closeOnce sync.Once
}

type flowKey struct {
	proto   uint8
	srcIP   [16]byte
	dstIP   [16]byte
	srcPort uint16
	dstPort uint16
}

func ipTo16(ip net.IP) [16]byte {
	var b [16]byte
	if ip == nil {
		return b
	}
	if ip4 := ip.To4(); ip4 != nil {
		// IPv4-mapped IPv6: ::ffff:a.b.c.d
		b[10] = 0xff
		b[11] = 0xff
		copy(b[12:], ip4)
	} else if len(ip) == 16 {
		copy(b[:], ip)
	}
	return b
}

// newPacketCounter opens an AF_PACKET socket and starts capturing.
// Returns nil if AF_PACKET is not available (e.g. no CAP_NET_RAW).
func newPacketCounter() *packetCounter {
	// ETH_P_ALL = 0x0003 (all protocols)
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(syscall.ETH_P_ALL)))
	if err != nil {
		log.Printf("sstop: AF_PACKET unavailable (need root/CAP_NET_RAW): %v", err)
		return nil
	}

	// Set receive buffer to 4MB for high-throughput capture
	syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4*1024*1024)

	// Set read timeout once so captureLoop can check stopCh periodically
	tv := syscall.Timeval{Sec: 0, Usec: 200_000} // 200ms
	syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)

	pc := &packetCounter{
		fd:     fd,
		flows:  make(map[flowKey]uint64),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}

	go pc.captureLoop()
	log.Printf("sstop: using AF_PACKET for per-connection bandwidth tracking")
	return pc
}

func (pc *packetCounter) close() {
	pc.closeOnce.Do(func() {
		close(pc.stopCh)
		<-pc.done // wait for goroutine to exit
		syscall.Close(pc.fd) // close fd AFTER goroutine exits
	})
}

func (pc *packetCounter) captureLoop() {
	defer close(pc.done)

	buf := make([]byte, 65536)

	for {
		select {
		case <-pc.stopCh:
			return
		default:
		}

		n, _, err := syscall.Recvfrom(pc.fd, buf, 0)
		if err != nil {
			// Timeout (EAGAIN/EWOULDBLOCK) or interrupted — check stop and retry
			continue
		}
		if n < 1 {
			continue
		}

		pc.processPacket(buf[:n])
	}
}

func (pc *packetCounter) processPacket(pkt []byte) {
	if len(pkt) < 1 {
		return
	}

	version := pkt[0] >> 4

	var proto uint8
	var srcIP, dstIP [16]byte
	var payloadOffset int
	var totalLen int

	switch version {
	case 4:
		if len(pkt) < 20 {
			return
		}
		ihl := int(pkt[0]&0x0f) * 4
		if len(pkt) < ihl {
			return
		}
		totalLen = int(binary.BigEndian.Uint16(pkt[2:4]))
		if totalLen > len(pkt) {
			totalLen = len(pkt) // use actual captured length
		}
		proto = pkt[9]
		// IPv4 → IPv4-mapped IPv6
		srcIP[10] = 0xff
		srcIP[11] = 0xff
		copy(srcIP[12:], pkt[12:16])
		dstIP[10] = 0xff
		dstIP[11] = 0xff
		copy(dstIP[12:], pkt[16:20])
		payloadOffset = ihl

	case 6:
		if len(pkt) < 40 {
			return
		}
		payloadLen := int(binary.BigEndian.Uint16(pkt[4:6]))
		totalLen = 40 + payloadLen
		if totalLen > len(pkt) {
			totalLen = len(pkt)
		}
		// Walk extension headers to find transport protocol
		proto = pkt[6] // Next Header
		payloadOffset = 40
		proto, payloadOffset = walkIPv6ExtHeaders(pkt, proto, payloadOffset)

	default:
		return
	}

	// Only track TCP (6) and UDP (17)
	if proto != 6 && proto != 17 {
		return
	}

	if len(pkt) < payloadOffset+4 {
		return
	}

	srcPort := binary.BigEndian.Uint16(pkt[payloadOffset : payloadOffset+2])
	dstPort := binary.BigEndian.Uint16(pkt[payloadOffset+2 : payloadOffset+4])

	key := flowKey{
		proto:   proto,
		srcIP:   srcIP,
		dstIP:   dstIP,
		srcPort: srcPort,
		dstPort: dstPort,
	}

	pc.mu.Lock()
	pc.flows[key] += uint64(totalLen)
	pc.mu.Unlock()
}

// walkIPv6ExtHeaders follows IPv6 extension header chain to find the transport protocol.
func walkIPv6ExtHeaders(pkt []byte, nextHdr uint8, offset int) (proto uint8, transportOffset int) {
	for i := 0; i < 8; i++ { // max 8 extension headers
		switch nextHdr {
		case 6, 17:
			// TCP or UDP — we're done
			return nextHdr, offset
		case 0, 43, 60: // Hop-by-Hop, Routing, Destination
			if len(pkt) < offset+2 {
				return nextHdr, offset
			}
			nextHdr = pkt[offset]
			extLen := int(pkt[offset+1]+1) * 8
			offset += extLen
		case 44: // Fragment
			if len(pkt) < offset+8 {
				return nextHdr, offset
			}
			nextHdr = pkt[offset]
			offset += 8
		default:
			// Unknown extension or not TCP/UDP
			return nextHdr, offset
		}
	}
	return nextHdr, offset
}

// getBytes returns cumulative bytes sent and received for a given socket.
// sent = bytes from localIP:localPort → remoteIP:remotePort
// recv = bytes from remoteIP:remotePort → localIP:localPort
func (pc *packetCounter) getBytes(proto uint8, localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16) (sent, recv uint64) {
	lIP := ipTo16(localIP)
	rIP := ipTo16(remoteIP)

	pc.mu.RLock()
	defer pc.mu.RUnlock()

	// Upload: local → remote
	upKey := flowKey{proto: proto, srcIP: lIP, dstIP: rIP, srcPort: localPort, dstPort: remotePort}
	sent = pc.flows[upKey]

	// Download: remote → local
	downKey := flowKey{proto: proto, srcIP: rIP, dstIP: lIP, srcPort: remotePort, dstPort: localPort}
	recv = pc.flows[downKey]

	return
}

// prune removes entries not in the given set of active flow keys.
// Called periodically to prevent unbounded memory growth.
func (pc *packetCounter) prune(active map[flowKey]bool) {
	if len(active) == 0 {
		return // don't purge everything when no active connections
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	for k := range pc.flows {
		if !active[k] {
			delete(pc.flows, k)
		}
	}
}

func htons(v uint16) uint16 {
	b := (*[2]byte)(unsafe.Pointer(&v))
	return binary.BigEndian.Uint16(b[:])
}
