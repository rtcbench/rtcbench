package aofconf

import (
	"call.zip/internal/aoflog"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	LogsDir     *string
	DumpPackets bool
	Steps       aoflog.Client
	Pion        aoflog.Client
	PktRecv     aoflog.Client
	BOSH        aoflog.Client
	Handshake   aoflog.Client
	KeepAlive   aoflog.Client
	NetStats    aoflog.Client
}

func NewStdClientConfig(serverIP, clientIP, confName, viewerName string) *Config {
	stepsAOF := aoflog.NewStdClient(fmt.Sprintf("%s->%s/%s:%s(steps)", serverIP, clientIP, confName, viewerName))
	pionAOF := aoflog.NewStdClient(fmt.Sprintf("%s->%s/%s:%s(pion)", serverIP, clientIP, confName, viewerName))
	pktRecvAOF := aoflog.NewStdClient(fmt.Sprintf("%s->%s/%s:%s(pktrecv)", serverIP, clientIP, confName, viewerName))
	boshAOF := aoflog.NewStdClient(fmt.Sprintf("%s->%s/%s:%s(bosh)", serverIP, clientIP, confName, viewerName))
	hsAOF := aoflog.NewStdClient(fmt.Sprintf("%s->%s/%s:%s(hs)", serverIP, clientIP, confName, viewerName))
	kaAOF := aoflog.NewStdClient(fmt.Sprintf("%s->%s/%s:%s(ka)", serverIP, clientIP, confName, viewerName))
	netStatsAOF := aoflog.NewStdClient(fmt.Sprintf("%s->%s/%s:%s(net)", serverIP, clientIP, confName, viewerName))

	return &Config{
		LogsDir:     nil,
		DumpPackets: false,
		Steps:       stepsAOF,
		Pion:        pionAOF,
		PktRecv:     pktRecvAOF,
		BOSH:        boshAOF,
		Handshake:   hsAOF,
		KeepAlive:   kaAOF,
		NetStats:    netStatsAOF,
	}
}

var (
	baseLogDir     string
	initBaseLogDir sync.Once
)

func getBaseLogDir() string {
	initBaseLogDir.Do(func() {
		timestamp := time.Now().Format("20060102T150405")
		baseLogDir = filepath.Join("logs", fmt.Sprintf("e-%s", timestamp))
	})
	return baseLogDir
}

func NewDebugLogsClientConfig(serverIP, clientIP, confName, viewerName string) *Config {
	baseDir := filepath.Join(getBaseLogDir(), "debug", viewerName)
	_ = os.MkdirAll(baseDir, 0755)

	combinedPath := filepath.Join(baseDir, "_combined.log")
	combinedClient, err := aoflog.NewFileClient("", combinedPath)
	if err != nil {
		log.Fatalf("[debug] failed to create combined log: %v", err)
	}

	makeClient := func(name string) aoflog.Client {
		path := filepath.Join(baseDir, name+".log")
		prefix := fmt.Sprintf("%s->%s/%s:%s(%s)", serverIP, clientIP, confName, viewerName, name)
		indivClient, err := aoflog.NewFileClient(prefix, path)
		if err != nil {
			log.Fatalf("[debug] failed to create %s log: %v", name, err)
		}
		return aoflog.NewMultiClient(indivClient, combinedClient)
	}

	makeStatsClient := func(name string) aoflog.Client {
		path := filepath.Join(baseDir, name+".csv")
		client, err := aoflog.NewFileClient("", path)
		if err != nil {
			log.Fatalf("[debug] failed to create %s log: %v", name, err)
		}
		return client
	}

	return &Config{
		LogsDir:     &baseDir,
		DumpPackets: true,
		Steps:       makeClient("steps"),
		Pion:        makeClient("pion"),
		PktRecv:     makeClient("pktrecv"),
		BOSH:        makeClient("bosh"),
		Handshake:   makeClient("hs"),
		KeepAlive:   makeClient("ka"),
		NetStats:    makeStatsClient("net-" + viewerName),
	}
}

func NewPerVBotLogsClientConfig(serverIP, clientIP, confName, viewerName string, dumpPackets bool) *Config {
	baseDir := filepath.Join(getBaseLogDir(), "vbot")
	_ = os.MkdirAll(baseDir, 0755)

	path := filepath.Join(baseDir, viewerName+".log")

	makeClient := func(name string) aoflog.Client {
		prefix := fmt.Sprintf("%s->%s/%s:%s(%s)", serverIP, clientIP, confName, viewerName, name)
		client, err := aoflog.NewFileClient(prefix, path)
		if err != nil {
			log.Fatalf("[vbot] failed to create %s log: %v\n", name, err)
		}
		return client
	}

	makeStatsClient := func(name string) aoflog.Client {
		path := filepath.Join(baseDir, name+".csv")
		client, err := aoflog.NewFileClient("", path)
		if err != nil {
			log.Fatalf("[vbot] failed to create %s log: %v", name, err)
		}
		return client
	}

	return &Config{
		LogsDir:     &baseDir,
		DumpPackets: dumpPackets,
		Steps:       makeClient("steps"),
		Pion:        makeClient("pion"),
		PktRecv:     makeClient("pktrecv"),
		BOSH:        makeClient("bosh"),
		Handshake:   makeClient("hs"),
		KeepAlive:   makeClient("ka"),
		NetStats:    makeStatsClient("net-" + viewerName),
	}
}

func NewNilClientConfig() *Config {
	return &Config{
		LogsDir:     nil,
		DumpPackets: false,
		Steps:       aoflog.NilClient{},
		Pion:        aoflog.NilClient{},
		PktRecv:     aoflog.NilClient{},
		BOSH:        aoflog.NilClient{},
		Handshake:   aoflog.NilClient{},
		KeepAlive:   aoflog.NilClient{},
		NetStats:    aoflog.NilClient{},
	}
}
