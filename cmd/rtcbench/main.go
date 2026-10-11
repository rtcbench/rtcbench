package main

import (
	"context"
	"log"
	"os"

	"github.com/rtcbench/rtcbench"
	"github.com/rtcbench/rtcbench/pkg/deployment"
	"github.com/rtcbench/rtcbench/plugin/janus"
	"github.com/rtcbench/rtcbench/plugin/jitsi"
	"github.com/rtcbench/rtcbench/plugin/livekit"
	"github.com/rtcbench/rtcbench/plugin/mediasoup"
)

var version = "dev"

func main() {
	err := rtcbench.Run(context.Background(), os.Args[1:], rtcbench.Options{
		Version:     version,
		Deployments: []deployment.Plugin{livekit.Deployment, janus.Deployment, mediasoup.Deployment, jitsi.Deployment},
		Plugins:     rtcbench.BuiltinPlugins,
		Register: func(client *rtcbench.Client) {
			client.RegisterPlugin(jitsi.PluginID, jitsi.NewPlugin)
			client.RegisterPlugin(janus.PluginID, janus.NewPlugin)
			client.RegisterPlugin(livekit.PluginID, livekit.NewPlugin)
			client.RegisterPlugin(mediasoup.PluginID, mediasoup.NewPlugin)
		},
	})
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
