package pionutil

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/pion/transport/v4/vnet"
)

type impairmentProxy struct {
	serverIP  net.IP
	serverNet *vnet.Net
	listeners sync.Map
}

func newImpairmentProxy(router *vnet.Router, serverIP net.IP, initialPort int) (*impairmentProxy, error) {
	serverNet, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{serverIP.String()},
	})
	if err != nil {
		return nil, fmt.Errorf("vnet.NewNet: %w", err)
	}
	if err = router.AddNet(serverNet); err != nil {
		return nil, fmt.Errorf("router.AddNet: %w", err)
	}

	proxy := &impairmentProxy{
		serverIP:  append(net.IP(nil), serverIP...),
		serverNet: serverNet,
	}

	router.AddChunkFilter(func(c vnet.Chunk) bool {
		proxy.ensureChunkDestination(c)
		return true
	})

	if initialPort > 0 {
		if err := proxy.ensurePort(initialPort); err != nil {
			return nil, err
		}
	}

	return proxy, nil
}

func (p *impairmentProxy) Close() error {
	var errs []error
	p.listeners.Range(func(_, value any) bool {
		if err := value.(*proxyPortForwarder).Close(); err != nil {
			errs = append(errs, err)
		}
		return true
	})
	return errors.Join(errs...)
}

func (p *impairmentProxy) ensureChunkDestination(c vnet.Chunk) {
	destination, ok := c.DestinationAddr().(*net.UDPAddr)
	if !ok || destination.Port <= 0 || !destination.IP.Equal(p.serverIP) {
		return
	}
	_ = p.ensurePort(destination.Port)
}

func (p *impairmentProxy) ensurePort(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("impairment: remote port %d out of range [1,65535]", port)
	}
	if _, ok := p.listeners.Load(port); ok {
		return nil
	}

	forwarder, err := newProxyPortForwarder(p.serverNet, p.serverIP, port)
	if err != nil {
		return err
	}

	actual, loaded := p.listeners.LoadOrStore(port, forwarder)
	if loaded {
		_ = forwarder.Close()
		return actual.(*proxyPortForwarder).Err()
	}

	return nil
}

type proxyPortForwarder struct {
	serverIP   net.IP
	port       int
	vnetSocket net.PacketConn
	endpoints  sync.Map
	closeOnce  sync.Once
	wg         sync.WaitGroup
	errMu      sync.Mutex
	err        error
}

func newProxyPortForwarder(serverNet *vnet.Net, serverIP net.IP, port int) (*proxyPortForwarder, error) {
	vnetSocket, err := serverNet.ListenUDP("udp4", &net.UDPAddr{IP: serverIP, Port: port})
	if err != nil {
		return nil, fmt.Errorf("listen on %s:%d: %w", serverIP.String(), port, err)
	}

	forwarder := &proxyPortForwarder{
		serverIP:   append(net.IP(nil), serverIP...),
		port:       port,
		vnetSocket: vnetSocket,
	}
	forwarder.wg.Add(1)
	go forwarder.run()

	return forwarder, nil
}

func (f *proxyPortForwarder) run() {
	defer f.wg.Done()

	buffer := make([]byte, 1500)
	for {
		n, addr, err := f.vnetSocket.ReadFrom(buffer)
		if err != nil {
			f.recordErr(err)
			return
		}
		if n <= 0 || addr == nil {
			continue
		}

		endpoint, err := f.endpointFor(addr)
		if err != nil {
			f.recordErr(err)
			continue
		}
		if _, err := endpoint.Write(buffer[:n]); err != nil {
			f.recordErr(err)
			continue
		}
	}
}

func (f *proxyPortForwarder) endpointFor(vnetClientAddr net.Addr) (*net.UDPConn, error) {
	key := vnetClientAddr.String()
	if value, ok := f.endpoints.Load(key); ok {
		return value.(*net.UDPConn), nil
	}

	realSocket, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: f.serverIP, Port: f.port})
	if err != nil {
		return nil, fmt.Errorf("dial %s:%d: %w", f.serverIP.String(), f.port, err)
	}

	actual, loaded := f.endpoints.LoadOrStore(key, realSocket)
	if loaded {
		_ = realSocket.Close()
		return actual.(*net.UDPConn), nil
	}

	f.wg.Add(1)
	go f.copyToVNet(cloneUDPAddr(vnetClientAddr), realSocket, key)

	return realSocket, nil
}

func (f *proxyPortForwarder) copyToVNet(vnetClientAddr net.Addr, realSocket *net.UDPConn, key string) {
	defer f.wg.Done()
	defer f.endpoints.Delete(key)
	defer realSocket.Close()

	buffer := make([]byte, 1500)
	for {
		n, _, err := realSocket.ReadFrom(buffer)
		if err != nil {
			f.recordErr(err)
			return
		}
		if n <= 0 {
			continue
		}
		if _, err := f.vnetSocket.WriteTo(buffer[:n], vnetClientAddr); err != nil {
			f.recordErr(err)
			return
		}
	}
}

func (f *proxyPortForwarder) Close() error {
	f.closeOnce.Do(func() {
		if err := f.vnetSocket.Close(); err != nil {
			f.recordErr(err)
		}
		f.endpoints.Range(func(_, value any) bool {
			if err := value.(*net.UDPConn).Close(); err != nil {
				f.recordErr(err)
			}
			return true
		})
		f.wg.Wait()
	})
	return f.Err()
}

func (f *proxyPortForwarder) Err() error {
	f.errMu.Lock()
	defer f.errMu.Unlock()
	return f.err
}

func (f *proxyPortForwarder) recordErr(err error) {
	if err == nil || errors.Is(err, net.ErrClosed) {
		return
	}

	f.errMu.Lock()
	defer f.errMu.Unlock()
	f.err = errors.Join(f.err, err)
}

func cloneUDPAddr(addr net.Addr) net.Addr {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return addr
	}
	return &net.UDPAddr{
		IP:   append(net.IP(nil), udpAddr.IP...),
		Port: udpAddr.Port,
		Zone: udpAddr.Zone,
	}
}
