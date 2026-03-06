package janus

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"

	"call.zip"
	janus "call.zip/plugin/janus/internal"
)

const (
	PluginID = "janus"

	cfgServerRoot    = "serverRoot"
	cfgAllowInsecure = "allowInsecureHttps"
)

type Plugin struct {
	client *janus.Client
	info   map[string]any
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, config *call.Config) error {
	baseURL := config.Spec.PluginConfig[PluginID].(map[string]any)[cfgServerRoot].(string)
	allowInsecure := config.Spec.PluginConfig[PluginID].(map[string]any)[cfgAllowInsecure].(bool)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if allowInsecure {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	httpClient := http.Client{
		Transport: transport,
	}

	p.client = janus.NewClient(&httpClient, baseURL)

	info, err := p.client.GetInfo()

	if err != nil {
		return err
	}

	log.Printf("Found Janus server: \"%v\"; Version: \"%v\"", info["name"], info["version_string"])
	p.info = info

	return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	log.Println("unimplemented: janus.Shutdown(ctx)")
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	log.Printf("unimplemented: janus.JoinRoom(ctx, %v, %v, %v)", role, roomID, userID)
	return nil
}
