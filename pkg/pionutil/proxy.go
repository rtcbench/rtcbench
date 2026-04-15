package pionutil

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/pion/transport/v4/vnet"
)

type impairmentProxy struct {
	serverIP   net.IP
	serverNet  *vnet.Net
	listeners  sync.Map
	profilesMu sync.RWMutex
	profiles   map[string]*ImpairmentProfile
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
		profiles:  make(map[string]*ImpairmentProfile),
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

func (p *impairmentProxy) registerProfile(clientIP string, profile *ImpairmentProfile) {
	if clientIP == "" || profile == nil {
		return
	}

	p.profilesMu.Lock()
	p.profiles[clientIP] = profile
	p.profilesMu.Unlock()
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

	forwarder, err := newProxyPortForwarder(p, port)
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

func (p *impairmentProxy) profileForAddr(addr net.Addr) *ImpairmentProfile {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok || udpAddr == nil {
		return nil
	}

	p.profilesMu.RLock()
	profile := p.profiles[udpAddr.IP.String()]
	p.profilesMu.RUnlock()
	return profile
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
	proxy      *impairmentProxy
}

func newProxyPortForwarder(proxy *impairmentProxy, port int) (*proxyPortForwarder, error) {
	vnetSocket, err := proxy.serverNet.ListenUDP("udp4", &net.UDPAddr{IP: proxy.serverIP, Port: port})
	if err != nil {
		return nil, fmt.Errorf("listen on %s:%d: %w", proxy.serverIP.String(), port, err)
	}

	forwarder := &proxyPortForwarder{
		serverIP:   append(net.IP(nil), proxy.serverIP...),
		port:       port,
		vnetSocket: vnetSocket,
		proxy:      proxy,
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
		endpoint.writeToReal(buffer[:n])
	}
}

func (f *proxyPortForwarder) endpointFor(vnetClientAddr net.Addr) (*proxyEndpoint, error) {
	key := vnetClientAddr.String()
	if value, ok := f.endpoints.Load(key); ok {
		return value.(*proxyEndpoint), nil
	}

	realSocket, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: f.serverIP, Port: f.port})
	if err != nil {
		return nil, fmt.Errorf("dial %s:%d: %w", f.serverIP.String(), f.port, err)
	}

	endpoint := newProxyEndpoint(
		f.vnetSocket,
		cloneUDPAddr(vnetClientAddr),
		realSocket,
		f.proxy.profileForAddr(vnetClientAddr),
		func(err error) { f.recordErr(err) },
	)

	actual, loaded := f.endpoints.LoadOrStore(key, endpoint)
	if loaded {
		endpoint.Close()
		return actual.(*proxyEndpoint), nil
	}

	f.wg.Add(1)
	go f.copyToVNet(endpoint, key)

	return endpoint, nil
}

func (f *proxyPortForwarder) copyToVNet(endpoint *proxyEndpoint, key string) {
	defer f.wg.Done()
	defer f.endpoints.Delete(key)
	defer endpoint.Close()

	buffer := make([]byte, 1500)
	for {
		n, _, err := endpoint.realSocket.ReadFrom(buffer)
		if err != nil {
			f.recordErr(err)
			return
		}
		if n <= 0 {
			continue
		}
		endpoint.writeToVNet(buffer[:n])
	}
}

func (f *proxyPortForwarder) Close() error {
	f.closeOnce.Do(func() {
		if err := f.vnetSocket.Close(); err != nil {
			f.recordErr(err)
		}
		f.endpoints.Range(func(_, value any) bool {
			value.(*proxyEndpoint).Close()
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

type proxyEndpoint struct {
	realSocket     *net.UDPConn
	vnetSocket     net.PacketConn
	vnetClientAddr net.Addr
	uplink         *packetScheduler
	downlink       *packetScheduler
	recordErr      func(error)
	closeOnce      sync.Once
}

func newProxyEndpoint(vnetSocket net.PacketConn, vnetClientAddr net.Addr, realSocket *net.UDPConn, profile *ImpairmentProfile, recordErr func(error)) *proxyEndpoint {
	endpoint := &proxyEndpoint{
		realSocket:     realSocket,
		vnetSocket:     vnetSocket,
		vnetClientAddr: vnetClientAddr,
		recordErr:      recordErr,
	}
	endpoint.uplink = newPacketScheduler(profile, vnetClientAddr.String()+"/uplink", func(data []byte) {
		if _, err := endpoint.realSocket.Write(data); err != nil {
			recordErr(err)
		}
	})
	endpoint.downlink = newPacketScheduler(profile, vnetClientAddr.String()+"/downlink", func(data []byte) {
		if _, err := endpoint.vnetSocket.WriteTo(data, endpoint.vnetClientAddr); err != nil {
			recordErr(err)
		}
	})
	return endpoint
}

func (e *proxyEndpoint) writeToReal(payload []byte) {
	if e.uplink == nil {
		if _, err := e.realSocket.Write(payload); err != nil {
			e.recordErr(err)
		}
		return
	}
	e.uplink.Enqueue(payload)
}

func (e *proxyEndpoint) writeToVNet(payload []byte) {
	if e.downlink == nil {
		if _, err := e.vnetSocket.WriteTo(payload, e.vnetClientAddr); err != nil {
			e.recordErr(err)
		}
		return
	}
	e.downlink.Enqueue(payload)
}

func (e *proxyEndpoint) Close() {
	e.closeOnce.Do(func() {
		if e.uplink != nil {
			e.uplink.Close()
		}
		if e.downlink != nil {
			e.downlink.Close()
		}
		_ = e.realSocket.Close()
	})
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
