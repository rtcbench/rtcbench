package bench

import (
	"fmt"
	"slices"
)

type RoomGroup struct {
	Offset int `json:"offset"`
	Rooms  int `json:"rooms"`
	Users  int `json:"usersPerRoom"`
}

type Topology struct {
	Load            int         `json:"loadPerMachine"`
	Rooms           int         `json:"rooms"`
	PerRoom         int         `json:"limitPerRoom"`
	TotalSenders    int         `json:"totalSenders"`
	TotalReceivers  int         `json:"totalReceivers"`
	ExpectedStreams int         `json:"expectedStreams"`
	Workers         []string    `json:"workers"`
	Observer        string      `json:"observer"`
	SFUIP           string      `json:"sfuIP"`
	Groups          []RoomGroup `json:"groups"`
}

func Layout(c Cluster, mode, sfu string, load int) (Topology, error) {
	if len(c.SFU) == 0 || !slices.Contains(SFUs, sfu) || (mode != "receiver" && mode != "sender") {
		return Topology{}, fmt.Errorf("invalid SFU or mode")
	}
	plugin, err := c.plugin(sfu, "")
	if err != nil {
		return Topology{}, err
	}
	t := Topology{
		Load:     load,
		PerRoom:  20,
		Workers:  append([]string{}, c.Receivers...),
		Observer: c.Sender,
		SFUIP:    c.DataIP(plugin.Host),
	}
	if mode == "sender" {
		t.Workers, t.Observer = append([]string{}, c.Senders...), c.Viewer
		t.PerRoom = plugin.SenderRoomSize
	}
	if load < 1 || t.PerRoom < 1 {
		return t, fmt.Errorf("invalid load or SFU")
	}
	t.Workers = slices.DeleteFunc(t.Workers, func(host string) bool {
		return slices.Contains(plugin.ReservedHosts, host)
	})
	if len(t.Workers) == 0 {
		return t, fmt.Errorf("no load machines remain after reserving SFU hosts")
	}
	if slices.Contains(t.Workers, t.Observer) {
		return t, fmt.Errorf("load machines and the observer/publisher must be separate")
	}
	seen := map[string]bool{}
	for _, host := range t.Workers {
		ip := c.DataIP(host)
		if seen[ip] {
			return t, fmt.Errorf("duplicate load machine %q", host)
		}
		seen[ip] = true
		if ip == c.DataIP(t.Observer) {
			return t, fmt.Errorf("load machines and the observer/publisher must be separate")
		}
		for _, server := range c.SFU {
			if ip == c.DataIP(server) {
				return t, fmt.Errorf("load machine %q overlaps an SFU host", host)
			}
		}
	}
	full, tail := load/t.PerRoom, load%t.PerRoom
	if full > 0 {
		t.Groups = append(t.Groups, RoomGroup{Rooms: full, Users: t.PerRoom})
	}
	if tail > 0 {
		t.Groups = append(t.Groups, RoomGroup{Offset: full, Rooms: 1, Users: tail})
	}
	t.Rooms = full
	if tail > 0 {
		t.Rooms++
	}
	t.ExpectedStreams = load * len(t.Workers)
	if mode == "sender" {
		t.TotalSenders, t.TotalReceivers = t.ExpectedStreams, t.Rooms
	} else {
		t.TotalSenders, t.TotalReceivers = t.Rooms, t.ExpectedStreams
	}
	return t, nil
}
