package pionutil

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestImpairmentRouterDiscoversRemotePort(t *testing.T) {
	serverIP, err := firstNonLoopbackIPv4()
	if err != nil {
		t.Fatalf("firstNonLoopbackIPv4: %v", err)
	}

	server, err := startUDPEchoServer(serverIP)
	if err != nil {
		t.Fatalf("startUDPEchoServer: %v", err)
	}
	defer server.Close()

	router, err := NewImpairmentRouter(serverIP.String(), 0)
	if err != nil {
		t.Fatalf("NewImpairmentRouter: %v", err)
	}
	if err := router.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer router.Stop()

	clientNet, err := buildImpairmentNet(&ImpairmentBinding{
		Router: router,
		Profile: &ImpairmentProfile{
			Name: "clean",
		},
	})
	if err != nil {
		t.Fatalf("buildImpairmentNet: %v", err)
	}

	conn, err := clientNet.DialUDP("udp4", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buffer := make([]byte, 16)
	n, _, err := conn.ReadFrom(buffer)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if string(buffer[:n]) != "ping" {
		t.Fatalf("ReadFrom() = %q, want ping", string(buffer[:n]))
	}
}

func TestImpairmentRouterAppliesDownlinkLatency(t *testing.T) {
	serverIP, err := firstNonLoopbackIPv4()
	if err != nil {
		t.Fatalf("firstNonLoopbackIPv4: %v", err)
	}

	server, err := startUDPEchoServer(serverIP)
	if err != nil {
		t.Fatalf("startUDPEchoServer: %v", err)
	}
	defer server.Close()

	router, err := NewImpairmentRouter(serverIP.String(), 0)
	if err != nil {
		t.Fatalf("NewImpairmentRouter: %v", err)
	}
	if err := router.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer router.Stop()

	clientNet, err := buildImpairmentNet(&ImpairmentBinding{
		Router: router,
		Profile: &ImpairmentProfile{
			Name:        "latency",
			BaseLatency: 60 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("buildImpairmentNet: %v", err)
	}

	conn, err := clientNet.DialUDP("udp4", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	started := time.Now()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buffer := make([]byte, 16)
	if _, _, err := conn.ReadFrom(buffer); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}

	if rtt := time.Since(started); rtt < 45*time.Millisecond {
		t.Fatalf("RTT = %s, want at least 45ms", rtt)
	}
}

func startUDPEchoServer(serverIP net.IP) (*net.UDPConn, error) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: serverIP, Port: 0})
	if err != nil {
		return nil, err
	}

	go func() {
		buffer := make([]byte, 1500)
		for {
			n, addr, err := server.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if _, err := server.WriteToUDP(buffer[:n], addr); err != nil {
				return
			}
		}
	}()

	return server, nil
}

func firstNonLoopbackIPv4() (net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip := ipNet.IP.To4(); ip != nil {
				return ip, nil
			}
		}
	}

	return nil, fmt.Errorf("no non-loopback IPv4 address found")
}
