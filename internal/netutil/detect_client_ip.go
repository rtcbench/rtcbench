package netutil

import (
	"errors"
	"fmt"
	"net"
)

var ErrDetectClientIP = errors.New("failed to detect client ip")

func DetectClientIP(serverIP string) (string, error) {
	ip := net.ParseIP(serverIP)
	if ip == nil {
		return "", fmt.Errorf("%w: invalid server ip %q", ErrDetectClientIP, serverIP)
	}

	network := "udp6"
	if ip.To4() != nil {
		network = "udp4"
	}

	c, err := net.Dial(network, net.JoinHostPort(serverIP, "9")) // port 9 is RFC863 "Discard Protocol"
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDetectClientIP, err)
	}
	defer c.Close()

	ua, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || ua.IP == nil {
		return "", fmt.Errorf("%w: unexpected local addr type %T", ErrDetectClientIP, c.LocalAddr())
	}

	if network == "udp4" {
		ip4 := ua.IP.To4()
		if ip4 == nil {
			return "", fmt.Errorf("%w: expected ipv4 local addr, got %v", ErrDetectClientIP, ua.IP)
		}
		return ip4.String(), nil
	}

	return ua.IP.String(), nil
}
