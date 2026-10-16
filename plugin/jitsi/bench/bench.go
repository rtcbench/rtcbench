package bench

import (
	"fmt"
	"net/url"
	"strconv"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

type Topology struct {
	JVB                        benchmark.Hosts `yaml:"jvb" json:"jvb"`
	Prosody                    string          `yaml:"prosody_web" json:"prosody_web"`
	Jicofo                     string          `yaml:"jicofo" json:"jicofo"`
	DisableBandwidthEstimation bool            `yaml:"disable_bandwidth_estimation,omitempty" json:"disable_bandwidth_estimation,omitempty"`
}

type Config struct {
	Jitsi    Topology `yaml:"jitsi,omitempty" json:"jitsi,omitempty"`
	JitsiWeb int      `yaml:"jitsi_web_port" json:"jitsi_web_port"`
}

func DefaultConfig() Config { return Config{JitsiWeb: 443} }

func New(e benchmark.Environment, c Config) benchmark.Plugin {
	hosts := append([]string{}, c.Jitsi.JVB...)
	for _, host := range []string{c.Jitsi.Prosody, c.Jitsi.Jicofo} {
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return benchmark.Plugin{
		ID:                "jitsi",
		Host:              c.Jitsi.Prosody,
		Hosts:             hosts,
		ReservedHosts:     hosts,
		SenderRoomSize:    15,
		SenderJoinLimit:   3,
		SenderJoinSpacing: "2s",
		Ports:             []benchmark.Port{{Value: c.JitsiWeb, Default: 443}},
		CheckHosts: func() error {
			if len(c.Jitsi.JVB) == 0 || c.Jitsi.Prosody == "" || c.Jitsi.Jicofo == "" {
				return fmt.Errorf("Jitsi needs jvb, prosody_web and jicofo hosts")
			}
			return nil
		},
		NativeConfig: func(ip string) map[string]any {
			return map[string]any{
				"_boshURL": benchmark.Address("https", ip, c.JitsiWeb) + "/http-bind",
				"_domain":  ip,
			}
		},
		BrowserURL: func(ip string, publish bool) string { return browserURL(c, ip, publish) },
		Services:   services(e, c.Jitsi),
	}
}

func browserURL(c Config, ip string, publish bool) string {
	base := benchmark.Address("https", ip, c.JitsiWeb)
	if !publish {
		return base + "/room-1234" +
			"#config.prejoinConfig.enabled=false" +
			"&config.p2p.enabled=false" +
			"&config.startWithAudioMuted=true" +
			"&config.startWithVideoMuted=true" +
			"&config.startSilent=true" +
			"&config.disableDeepLinking=true" +
			"&config.requireDisplayName=false" +
			"&config.testing.testMode=true" +
			"&config.testing.noAutoPlayVideo=true" +
			"&config.channelLastN=-1" +
			"&config.notifications=[]" +
			"&userInfo.displayName=%22bench-viewer%22"
	}
	params := url.Values{
		"room":    {"room-1234"},
		"publish": {strconv.FormatBool(publish)},
		"receive": {strconv.FormatBool(!publish)},
	}
	return base + "/bench.html?" + params.Encode()
}

func services(e benchmark.Environment, c Topology) []benchmark.Service {
	domain := e.DataIP(c.Prosody)
	common := []string{
		"ENABLE_AUTH=0",
		"ENABLE_GUESTS=1",
		"XMPP_DOMAIN=" + domain,
		"XMPP_AUTH_DOMAIN=auth." + domain,
		"XMPP_MUC_DOMAIN=conference." + domain,
		"XMPP_RECORDER_DOMAIN=recorder." + domain,
		"XMPP_INTERNAL_MUC_DOMAIN=internal-muc." + domain,
		"TZ=UTC",
	}
	makeService := func(host, name, image, health string, env ...string) benchmark.Service {
		return benchmark.Service{
			Host:   host,
			Name:   e.Name(name),
			Image:  image,
			Health: health,
			Env:    append(append([]string{}, common...), env...),
		}
	}
	result := []benchmark.Service{
		makeService(
			c.Prosody, "prosody", "jitsi/prosody:stable-9646",
			"timeout 2 bash -c 'echo > /dev/tcp/localhost/5222'",
			"AUTH_TYPE=internal",
			"JICOFO_AUTH_PASSWORD=jicofosecret",
			"JVB_AUTH_PASSWORD=jvbsecret",
		),
		makeService(
			c.Prosody, "jitsi-web", "rtcbench-jitsi-web:latest",
			"curl -fkSs --max-time 2 https://localhost/",
			"XMPP_SERVER=127.0.0.1",
			"XMPP_BOSH_URL_BASE=http://127.0.0.1:5280",
			"PUBLIC_URL=https://"+domain,
		),
		{
			Host:  c.Prosody,
			Name:  e.Name("jitsi-sender-web"),
			Image: "rtcbench-jitsi-bench-web:latest",
		},
		makeService(
			c.Jicofo, "jicofo", "jitsi/jicofo:stable-9646",
			"curl -fsS --max-time 2 http://localhost:8888/about/health",
			"AUTH_TYPE=internal",
			"XMPP_SERVER="+domain,
			"XMPP_PORT=5222",
			"JICOFO_AUTH_USER=focus",
			"JICOFO_AUTH_PASSWORD=jicofosecret",
			"JICOFO_ENABLE_HEALTH_CHECKS=true",
		),
	}
	for i, host := range c.JVB {
		ip := e.DataIP(host)
		name := fmt.Sprintf("jvb-%d", i)
		bridge := makeService(
			host, name, "jitsi/jvb:stable-9646",
			"curl -fsS --max-time 2 http://localhost:8080/about/health",
			"XMPP_SERVER="+domain,
			"XMPP_PORT=5222",
			"JVB_AUTH_USER=jvb",
			"JVB_AUTH_PASSWORD=jvbsecret",
			"JVB_ADVERTISE_IPS="+ip,
			"JVB_PORT=10000",
			"JVB_MUC_NICKNAME="+name,
			"JVB_TCP_HARVESTER_DISABLED=true",
			"JVB_STUN_SERVERS=",
			"ENABLE_COLIBRI_WEBSOCKET=1",
			"JVB_WS_DOMAIN="+ip+":9090",
			"JVB_WS_TLS=0",
		)
		if c.DisableBandwidthEstimation {
			bridge.Env = append(bridge.Env, "JVB_CC_TRUST_BWE=false")
		}
		result = append(result, bridge)
	}
	return result
}
