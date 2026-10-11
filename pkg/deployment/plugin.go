package deployment

import "fmt"

type Port struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Default  int    `json:"default"`
	MinFrom  string `json:"minFrom,omitempty"`
}

type Config struct {
	ID      string
	Plugin  string
	Address string
	Ports   map[string]int
	APIKey  string
	Secret  string
}

type WebUI struct {
	Port    string                                                  `json:"-"`
	HTTPS   bool                                                    `json:"-"`
	Join    func(Config, string, string) (map[string]string, error) `json:"-"`
	Rewrite func(Config, []byte, string) []byte                     `json:"-"`
}

type Object = map[string]any

// Plugin describes how to deploy an SFU and connect a benchmark to it
type Plugin struct {
	WebUI           *WebUI `json:"webUI,omitempty"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Ports           []Port `json:"ports"`
	PortNote        string `json:"portNote,omitempty"`
	Credentials     bool   `json:"credentials,omitempty"`
	TerminalService string `json:"-"`

	ValidatePorts func(map[string]int, bool) error                    `json:"-"`
	Bind          func(Config) (map[string]any, map[string]string)    `json:"-"`
	Recipe        func(Config) (map[string]Object, map[string][]byte) `json:"-"`
	CheckHost     func(Config, func(string) (string, error)) error    `json:"-"`
}

func (p Plugin) ResolvePorts(overrides map[string]int, managed bool) (map[string]int, error) {
	ports := make(map[string]int, len(p.Ports))
	for _, port := range p.Ports {
		ports[port.ID] = port.Default
	}
	for key, value := range overrides {
		if _, ok := ports[key]; !ok {
			return nil, fmt.Errorf("unknown %s port: %s", p.ID, key)
		}
		ports[key] = value
	}
	for _, port := range p.Ports {
		value := ports[port.ID]
		if value < 1 || value > 65535 {
			return nil, fmt.Errorf("%s port must be between 1 and 65535", port.ID)
		}
		if port.MinFrom != "" && value < ports[port.MinFrom] {
			return nil, fmt.Errorf("%s must not be less than %s", port.Name, port.MinFrom)
		}
	}
	if p.ValidatePorts != nil {
		if err := p.ValidatePorts(ports, managed); err != nil {
			return nil, err
		}
	}
	return ports, nil
}

func Service(image, probe string) Object {
	return Object{
		"image":        image,
		"network_mode": "host",
		"restart":      "unless-stopped",
		"ulimits":      Object{"nofile": Object{"soft": 65536, "hard": 65536}},
		"healthcheck": Object{
			"test":         []string{"CMD-SHELL", probe},
			"interval":     "5s",
			"timeout":      "5s",
			"start_period": "30s",
			"retries":      30,
		},
	}
}

func BuiltService(d Config, probe string) Object {
	s := Service("rtcbench-managed-"+d.Plugin+":"+d.ID, probe)
	s["build"] = Object{"context": "./" + d.Plugin}
	return s
}
