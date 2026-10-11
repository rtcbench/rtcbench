package deployment

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/google/uuid"
)

type Network struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

type Interface struct {
	NetworkID string `json:"networkId"`
	Address   string `json:"address"`
}

func (m *Manager) saveNetwork(n Network) (Network, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(n.CIDR))
	n.Name = strings.TrimSpace(n.Name)
	if err != nil || !p.Addr().Is4() || n.Name == "" || len(n.Name) > 80 {
		return Network{}, errors.New("provide a name and IPv4 CIDR")
	}
	n.ID, n.CIDR = uuid.NewString(), p.Masked().String()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, other := range m.db.Networks {
		if strings.EqualFold(other.Name, n.Name) {
			return Network{}, errors.New("network name already exists")
		}
	}
	return n, saveRecord(m, m.db.Networks, n.ID, n)
}

func (m *Manager) checkAddress(i Interface) error {
	n, ok := m.db.Networks[i.NetworkID]
	p, err := netip.ParsePrefix(n.CIDR)
	ip, ipErr := netip.ParseAddr(i.Address)
	if !ok || err != nil || ipErr != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || !p.Contains(ip) {
		return errors.New("traffic IP must belong to the selected network")
	}
	// /31 and /32 have no reserved network/broadcast host addresses
	if p.Bits() < 31 {
		last := p.Masked().Addr().As4()
		for bit := p.Bits(); bit < 32; bit++ {
			last[bit/8] |= 1 << (7 - bit%8)
		}
		if ip == p.Masked().Addr() || ip == netip.AddrFrom4(last) {
			return errors.New("use a host IP, not the network or broadcast address")
		}
	}
	return nil
}
