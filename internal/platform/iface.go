package platform

import "net"

// DetectDefaultInterface returns the name of the interface used for the default route.
// Falls back to the first non-loopback interface with a valid IP.
func DetectDefaultInterface() string {
	// Strategy: find which interface has a route to an external IP.
	// Connect a UDP socket (no actual traffic) to a public IP and see which local address is used.
	conn, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return fallbackInterface()
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	targetIP := localAddr.IP

	// Find which interface owns this IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.Equal(targetIP) {
				return iface.Name
			}
		}
	}

	return fallbackInterface()
}

// fallbackInterface returns the first non-loopback UP interface.
func fallbackInterface() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		if len(addrs) > 0 {
			return iface.Name
		}
	}
	return ""
}
