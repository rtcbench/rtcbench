package bench

import (
	"context"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type Hosts []string

func (h *Hosts) UnmarshalYAML(data []byte) error {
	var single string
	if err := yaml.Unmarshal(data, &single); err == nil {
		*h = []string{single}
		return nil
	}
	var many []string
	if err := yaml.Unmarshal(data, &many); err != nil {
		return err
	}
	*h = many
	return nil
}

type Remote interface {
	Run(context.Context, string, string) (string, error)
	Write(context.Context, string, string, []byte) error
}

type Environment struct {
	ID          string
	SFU         []string
	DataIPs     map[string]string
	BinariesDir string
}

func (e Environment) Host() string {
	if len(e.SFU) == 0 {
		return ""
	}
	return e.SFU[0]
}

func (e Environment) DataIP(host string) string {
	if ip := e.DataIPs[host]; ip != "" {
		return ip
	}
	return host
}

func (e Environment) Directory() string         { return path.Join("/dev/shm/rtcbench-bench", e.ID) }
func (e Environment) Name(suffix string) string { return "rtcbench-" + e.ID + "-" + suffix }

type Port struct {
	Value   int
	Default int
}

type Service struct {
	Host   string
	Name   string
	Image  string
	Health string
	Env    []string
}

type Artifact struct {
	Host string
	Path string
	Name string
}

type Asset struct {
	Local      string
	Remote     string
	Hosts      []string
	Executable bool
}

type Plugin struct {
	ID                string
	Host              string
	Hosts             []string
	ReservedHosts     []string
	SenderRoomSize    int
	SenderJoinLimit   int
	SenderJoinSpacing string
	Ports             []Port
	CheckHosts        func() error
	NativeConfig      func(string) map[string]any
	BrowserURL        func(string, bool) string
	Services          []Service
	Start             func(context.Context, Remote) error
	Stop              func(context.Context, Remote) error
	PrepareRooms      func(context.Context, Remote, int) error
	Artifacts         []Artifact
	Assets            []Asset
}

func (p Plugin) Validate(managed bool) error {
	if p.CheckHosts != nil {
		if err := p.CheckHosts(); err != nil {
			return err
		}
	}
	for _, port := range p.Ports {
		if port.Value < 1 || port.Value > 65535 {
			return fmt.Errorf("%s ports must be between 1 and 65535", p.ID)
		}
		if managed && port.Value != port.Default {
			return fmt.Errorf("%s deployment helper requires default ports; disable provisioning to use an existing custom deployment", p.ID)
		}
	}
	return nil
}

func Address(scheme, host string, port int) string {
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func Quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func Ready(ctx context.Context, remote Remote, host, check string) error {
	command := "for i in $(seq 60); do " + check + " >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1"
	if _, err := remote.Run(ctx, host, command); err != nil {
		return fmt.Errorf("SFU readiness on %s: %w", host, err)
	}
	return nil
}
