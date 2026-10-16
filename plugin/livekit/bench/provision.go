package bench

import (
	"context"
	"errors"
	"fmt"
	"path"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

func start(ctx context.Context, remote benchmark.Remote, e benchmark.Environment) error {
	if err := routes(ctx, remote, e, true); err != nil {
		return err
	}
	host := e.Host()
	config := fmt.Sprintf(`port: 7880
rtc:
  port_range_start: 10000
  port_range_end: 60000
  use_external_ip: false
  node_ip: %s
  congestion_control:
    stream_allocator:
      min_channel_capacity: 10000000000
keys:
  devkey: secret-secret-secret-secret-secret
room:
  auto_create: true
  departure_timeout: 5
  empty_timeout: 10
logging:
  level: info
`, e.DataIP(host))
	dir := e.Directory()
	if err := remote.Write(ctx, host, path.Join(dir, "livekit.yml"), []byte(config)); err != nil {
		return err
	}
	command := "ulimit -n 65536; nohup /tmp/bench-bin/livekit-server --config " +
		benchmark.Quote(path.Join(dir, "livekit.yml")) +
		" >" + benchmark.Quote(path.Join(dir, "livekit.log")) +
		" 2>&1 </dev/null & echo $! >" + benchmark.Quote(path.Join(dir, "livekit.pid"))
	if _, err := remote.Run(ctx, host, "sh -c "+benchmark.Quote(command)); err != nil {
		return err
	}
	return benchmark.Ready(ctx, remote, host, "curl -fsS --max-time 2 http://localhost:7880/")
}

func stop(ctx context.Context, remote benchmark.Remote, e benchmark.Environment) error {
	pidfile := benchmark.Quote(path.Join(e.Directory(), "livekit.pid"))
	command := "if test -f " + pidfile + "; then pid=$(cat " + pidfile + "); case $pid in ''|*[!0-9]*) exit 1;; esac; " +
		"if test \"$(readlink /proc/$pid/exe)\" = /tmp/bench-bin/livekit-server && " +
		"tr '\\0' '\\n' </proc/$pid/cmdline | grep -Fx -- " +
		benchmark.Quote(path.Join(e.Directory(), "livekit.yml")) + " >/dev/null; then kill \"$pid\"; " +
		"for i in $(seq 300); do test -e /proc/$pid/exe || break; sleep 0.1; done; " +
		"if test -e /proc/$pid/exe; then echo 'LiveKit did not stop' >&2; exit 1; fi; fi; rm -f " + pidfile + "; fi"
	_, err := remote.Run(ctx, e.Host(), command)
	return errors.Join(err, routes(ctx, remote, e, false))
}
