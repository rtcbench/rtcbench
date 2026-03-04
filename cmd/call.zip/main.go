package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"call.zip"
	"call.zip/internal/netutil"
	"call.zip/plugin/janus"
	"call.zip/plugin/jitsi"
	"github.com/goccy/go-yaml"
)

func main() {
	var (
		err                           error
		cfg                           *call.Config
		client                        *call.Client
		detectedClientIP              string
		sigtermCtx, shutdownCtx       context.Context
		sigtermCancel, shutdownCancel context.CancelFunc
	)

	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s config.yml", os.Args[0])
	}

	cfg, err = loadYAMLConfig(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to load YAML config file: %v", err)
	}

	if cfg.Spec.Network.ClientIP == "" {
		detectedClientIP, err = netutil.DetectClientIP(cfg.Spec.Network.ServerIP)
		if err != nil {
			log.Fatalf("Failed to detect client IP, please manually configure it: %v", err)
		}
		log.Printf("Detected client IP: %s", detectedClientIP)
		cfg.Spec.Network.ClientIP = detectedClientIP
	}

	client = call.NewClient(cfg)
	client.RegisterPlugin("jitsi", jitsi.NewPlugin)
	client.RegisterPlugin(janus.PluginID, janus.NewPlugin)

	err = client.JoinAllRooms(context.Background())
	if err != nil {
		log.Printf("[cli-main] error joining all rooms: %v", err)
		if errors.Is(err, call.ErrUnknownPlugin) {
			os.Exit(1)
		}
	}

	log.Println("[cli-main] finished joining rooms")

	sigtermCtx, sigtermCancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer sigtermCancel()
	<-sigtermCtx.Done()

	log.Println("[cli-main] signal received, shutting down...")

	shutdownCtx, shutdownCancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err = client.Shutdown(shutdownCtx); err != nil {
		log.Println("[cli-main] shutdown error:", err)
	} else {
		log.Println("[cli-main] shutdown complete")
	}
}

func loadYAMLConfig(yamlFile string) (*call.Config, error) {
	var (
		bytes      []byte
		yamlConfig call.YAMLConfig
		err        error
	)

	bytes, err = os.ReadFile(yamlFile)
	if err != nil {
		return nil, err
	}

	bytes, err = RenderEnvYAML(bytes)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(bytes, &yamlConfig)
	if err != nil {
		return nil, err
	}

	return yamlConfig.IntoConfig()
}
