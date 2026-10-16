package bench

import (
	"fmt"
	"math"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

var SFUs = pluginIDs()
var Clients = []string{"rtcbench", "webrtcperf", "chromium"}

type Hosts = benchmark.Hosts

type Cluster struct {
	SSHKey           string            `yaml:"ssh_key" json:"ssh_key"`
	SSHUser          string            `yaml:"ssh_user" json:"ssh_user"`
	Sender           string            `yaml:"sender,omitempty" json:"sender,omitempty"`
	Viewer           string            `yaml:"viewer,omitempty" json:"viewer,omitempty"`
	SFU              Hosts             `yaml:"sfu" json:"sfu"`
	Receivers        Hosts             `yaml:"receivers,omitempty" json:"receivers,omitempty"`
	Senders          Hosts             `yaml:"senders,omitempty" json:"senders,omitempty"`
	DataIPs          map[string]string `yaml:"data_ips,omitempty" json:"data_ips,omitempty"`
	JanusConfig      `yaml:",inline"`
	JitsiConfig      `yaml:",inline"`
	LiveKitConfig    `yaml:",inline"`
	MediasoupConfig  `yaml:",inline"`
	NICMbps          float64        `yaml:"nic_mbps,omitempty" json:"nic_mbps,omitempty"`
	VideoMbps        float64        `yaml:"video_bitrate_mbps" json:"video_bitrate_mbps"`
	InitialHi        map[string]int `yaml:"initial_hi,omitempty" json:"initial_hi,omitempty"`
	StartupSeconds   int            `yaml:"startup_timeout_seconds" json:"startup_timeout_seconds"`
	WarmupSeconds    int            `yaml:"warmup_seconds" json:"warmup_seconds"`
	DurationSeconds  int            `yaml:"steady_state_seconds" json:"steady_state_seconds"`
	StartLeadSeconds int            `yaml:"start_lead_seconds" json:"start_lead_seconds"`
	SampleSeconds    int            `yaml:"sample_seconds" json:"sample_seconds"`
	TeardownSeconds  int            `yaml:"teardown_seconds" json:"teardown_seconds"`
	MinBitrate       float64        `yaml:"min_bitrate_bps" json:"min_bitrate_bps"`
	MinFPS           float64        `yaml:"min_fps" json:"min_fps"`
	HealthyFraction  float64        `yaml:"healthy_fraction" json:"healthy_fraction"`
	Cores            string         `yaml:"cores,omitempty" json:"cores,omitempty"`
	RTCBenchImage    string         `yaml:"rtcbench_image" json:"rtcbench_image"`
	BrowserImage     string         `yaml:"webrtcperf_image" json:"webrtcperf_image"`
	RemoteIVF        string         `yaml:"remote_ivf_dir" json:"remote_ivf_dir"`
	VideoPath        string         `yaml:"video_path" json:"video_path"`
	BinariesDir      string         `yaml:"binaries_dir" json:"binaries_dir"`
	IVFDir           string         `yaml:"ivf_dir" json:"ivf_dir"`
}

type Cell struct {
	SFU    string `json:"sfu"`
	Client string `json:"client"`
}

func (c Cell) Key() string { return c.SFU + "/" + c.Client }

type Request struct {
	Name         string         `json:"name"`
	ClusterYAML  string         `json:"cluster"`
	Mode         string         `json:"mode"`
	Cells        []Cell         `json:"cells"`
	Bounds       Bounds         `json:"bounds"`
	Repetitions  int            `json:"repetitions"`
	Provision    bool           `json:"provision"`
	UploadAssets bool           `json:"uploadAssets"`
	DryRun       bool           `json:"dryRun"`
	Hints        map[string]int `json:"hints,omitempty"`
}

func ParseCluster(data string) (Cluster, error) {
	defaults := defaultPluginConfig()
	c := Cluster{
		JanusConfig:      defaults.JanusConfig,
		JitsiConfig:      defaults.JitsiConfig,
		LiveKitConfig:    defaults.LiveKitConfig,
		MediasoupConfig:  defaults.MediasoupConfig,
		StartupSeconds:   300,
		WarmupSeconds:    90,
		DurationSeconds:  300,
		StartLeadSeconds: 15,
		SampleSeconds:    5,
		TeardownSeconds:  10,
		MinBitrate:       2_800_000,
		MinFPS:           20,
		HealthyFraction:  1,
		VideoMbps:        3.5,
		RTCBenchImage:    "rtcbench:latest",
		BrowserImage:     "ghcr.io/vpalmisano/webrtcperf:latest",
		RemoteIVF:        "/opt/ivf-videos",
		VideoPath:        "/opt/ivf-videos/1080p30.mp4",
		BinariesDir:      "bin",
		IVFDir:           "ivf",
	}
	if strings.TrimSpace(data) == "" {
		return c, nil
	}
	if err := yaml.UnmarshalWithOptions([]byte(data), &c, yaml.Strict()); err != nil {
		return c, err
	}
	return c, nil
}

var sshUser = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]*[$]?$`)
var hostName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)
var coreList = regexp.MustCompile(`^[0-9]+(-[0-9]+)?(,[0-9]+(-[0-9]+)?)*$`)

func (r Request) Validate() (Cluster, error) {
	c, err := ParseCluster(r.ClusterYAML)
	if err != nil {
		return c, fmt.Errorf("cluster: %w", err)
	}
	if r.Mode != "receiver" && r.Mode != "sender" {
		return c, fmt.Errorf("mode must be receiver or sender")
	}
	if r.Bounds.Min < 1 || r.Bounds.Max < r.Bounds.Min || r.Bounds.Max > 1_000_000 {
		return c, fmt.Errorf("invalid search bounds")
	}
	if r.Repetitions < 1 || r.Repetitions > 10 {
		return c, fmt.Errorf("repetitions must be between 1 and 10")
	}
	if len(r.Cells) == 0 || len(r.Cells) > 12 {
		return c, fmt.Errorf("select between 1 and 12 SFU/client combinations")
	}
	seen := map[string]bool{}
	for _, cell := range r.Cells {
		if !slices.Contains(SFUs, cell.SFU) || !slices.Contains(Clients, cell.Client) || seen[cell.Key()] {
			return c, fmt.Errorf("invalid or duplicate combination %q", cell.Key())
		}
		seen[cell.Key()] = true
		plugin, err := c.plugin(cell.SFU, "")
		if err != nil {
			return c, err
		}
		if err := plugin.Validate(r.Provision); err != nil {
			return c, err
		}
	}
	if c.SSHKey == "" || !sshUser.MatchString(c.SSHUser) || len(c.SFU) == 0 {
		return c, fmt.Errorf("cluster needs ssh_key, ssh_user and sfu")
	}
	if r.Mode == "receiver" && (c.Sender == "" || len(c.Receivers) == 0) {
		return c, fmt.Errorf("receiver mode needs sender and receivers")
	}
	if r.Mode == "sender" && (c.Viewer == "" || len(c.Senders) == 0) {
		return c, fmt.Errorf("sender mode needs viewer and senders")
	}
	for _, host := range c.Hosts() {
		if !validHost(host) {
			return c, fmt.Errorf("invalid host %q", host)
		}
	}
	for host, ip := range c.DataIPs {
		if !validHost(host) || net.ParseIP(ip) == nil {
			return c, fmt.Errorf("invalid data_ips entry %q", host)
		}
		if r.Provision && (net.ParseIP(host).To4() == nil || net.ParseIP(ip).To4() == nil) {
			return c, fmt.Errorf("provisioning data-plane routes requires IPv4 SSH and data addresses")
		}
	}
	if c.StartupSeconds < 1 || c.StartupSeconds > 86400 ||
		c.WarmupSeconds < 0 || c.StartLeadSeconds < 0 || c.TeardownSeconds < 0 ||
		c.DurationSeconds < 1 || c.DurationSeconds > 86400 ||
		c.SampleSeconds < 1 || c.SampleSeconds > c.DurationSeconds {
		return c, fmt.Errorf("invalid warmup, measurement or sampling duration")
	}
	if c.WarmupSeconds > 86400 || c.StartLeadSeconds > 86400 || c.TeardownSeconds > 86400 {
		return c, fmt.Errorf("wait durations must not exceed 24 hours")
	}
	if c.MinBitrate <= 0 || c.MinFPS <= 0 || c.HealthyFraction <= 0 || c.HealthyFraction > 1 || c.NICMbps < 0 || c.VideoMbps <= 0 {
		return c, fmt.Errorf("invalid quality thresholds or NIC capacity")
	}
	for _, value := range []float64{c.MinBitrate, c.MinFPS, c.HealthyFraction, c.NICMbps, c.VideoMbps} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return c, fmt.Errorf("thresholds must be finite")
		}
	}
	if err := validateCPUs(c.Cores); err != nil {
		return c, fmt.Errorf("cores must be a CPU list such as 0-7 or 0,2,4,6")
	}
	for _, plugin := range c.plugins("") {
		for _, port := range plugin.Ports {
			if port.Value < 1 || port.Value > 65535 {
				return c, fmt.Errorf("ports must be between 1 and 65535")
			}
		}
	}
	for _, path := range []string{c.RemoteIVF, c.VideoPath} {
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\x00\r\n:") {
			return c, fmt.Errorf("remote media paths must be absolute Linux paths without colons")
		}
	}
	if c.RTCBenchImage == "" || c.BrowserImage == "" {
		return c, fmt.Errorf("container images must not be empty")
	}
	if r.UploadAssets && !r.Provision {
		return c, fmt.Errorf("uploadAssets requires provisioning")
	}

	for client, upper := range c.InitialHi {
		if !slices.Contains(Clients, client) || upper < r.Bounds.Min {
			return c, fmt.Errorf("invalid initial_hi for %s", client)
		}
	}
	for _, cell := range r.Cells {
		if _, err := Layout(c, r.Mode, cell.SFU, r.Bounds.Min); err != nil {
			return c, err
		}
	}
	return c, nil
}

func validateCPUs(value string) error {
	if value == "" {
		return nil
	}
	if !coreList.MatchString(value) {
		return fmt.Errorf("invalid CPU list")
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(value, ",") {
		first, last, rangeValue := strings.Cut(part, "-")
		lo, err := strconv.Atoi(first)
		if err != nil {
			return err
		}
		hi := lo
		if rangeValue {
			hi, err = strconv.Atoi(last)
			if err != nil {
				return err
			}
		}
		if lo > hi || hi > 65535 {
			return fmt.Errorf("invalid CPU range")
		}
		for n := lo; n <= hi; n++ {
			if seen[n] {
				return fmt.Errorf("duplicate CPU %d", n)
			}
			seen[n] = true
		}
	}
	return nil
}

func validHost(host string) bool { return net.ParseIP(host) != nil || hostName.MatchString(host) }
func (c Cluster) DataIP(host string) string {
	if ip := c.DataIPs[host]; ip != "" {
		return ip
	}
	return host
}
func (c Cluster) Hosts() []string {
	hosts := append([]string{}, c.SFU...)
	hosts = append(hosts, c.Senders...)
	hosts = append(hosts, c.Receivers...)
	for _, plugin := range c.plugins("") {
		hosts = append(hosts, plugin.Hosts...)
	}
	for _, h := range []string{c.Sender, c.Viewer} {
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	slices.Sort(hosts)
	return slices.Compact(hosts)
}

func ParseCells(value string) ([]Cell, error) {
	var cells []Cell
	for _, group := range strings.Split(value, ";") {
		sfu, clients, ok := strings.Cut(strings.TrimSpace(group), ":")
		if !ok || !slices.Contains(SFUs, sfu) {
			return nil, fmt.Errorf("expected sfu:client[,client], got %q", group)
		}
		for _, client := range strings.Split(clients, ",") {
			client = strings.TrimSpace(client)
			if !slices.Contains(Clients, client) {
				return nil, fmt.Errorf("unknown client %q", client)
			}
			cells = append(cells, Cell{SFU: sfu, Client: client})
		}
	}
	return cells, nil
}
