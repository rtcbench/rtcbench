package bench

import (
	"context"
	"errors"
	"maps"
	"slices"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

func routes(ctx context.Context, remote benchmark.Remote, e benchmark.Environment, add bool) error {
	hosts := slices.Sorted(maps.Keys(e.DataIPs))
	var result error
	apply := func(host, table, chain, rule string) error {
		base := "sudo -n iptables " + table
		rule += " -m comment --comment " + benchmark.Quote("rtcbench-"+e.ID)
		command := base + " -C " + chain + rule + " 2>/dev/null"
		if add {
			command += " || " + base + " -I " + chain + rule
		} else {
			command = base + " -S >/dev/null && if " + command + "; then " + base + " -D " + chain + rule + "; fi"
		}
		_, err := remote.Run(ctx, host, command)
		return err
	}
	for _, host := range hosts {
		for _, peer := range hosts {
			if host == peer {
				continue
			}
			rule := " -p udp -s " + benchmark.Quote(host) +
				" -d " + benchmark.Quote(e.DataIP(peer)) +
				" -j SNAT --to-source " + benchmark.Quote(e.DataIP(host))
			err := apply(host, "-t nat", "POSTROUTING", rule)
			if err != nil && add {
				return err
			}
			result = errors.Join(result, err)
		}
	}
	for _, host := range e.SFU {
		for _, peer := range hosts {
			if peer == host || e.DataIP(peer) == peer {
				continue
			}
			for _, rule := range []struct{ chain, args string }{
				{"INPUT", " -p udp -s " + benchmark.Quote(peer) + " --dport 10000:60000 -j DROP"},
				{"OUTPUT", " -p udp -d " + benchmark.Quote(peer) + " --sport 10000:60000 -j DROP"},
			} {
				err := apply(host, "", rule.chain, rule.args)
				if err != nil && add {
					return err
				}
				result = errors.Join(result, err)
			}
		}
	}
	return result
}
