package main

import (
	"flag"
	"log"

	"call.zip/internal/aofconf"
	"call.zip/internal/bosh"
	"call.zip/internal/muc"
)

const (
	defaultServerIP = "127.0.0.1"
	defaultClientIP = "127.0.0.1"
	defaultLogMode  = "std"
)

func main() {
	serverIPFlag := flag.String("server-ip", defaultServerIP, "conference server LAN IP")
	clientIPFlag := flag.String("client-ip", defaultClientIP, "this machines LAN IP")
	loggerFlag := flag.String("log", defaultLogMode, "how to log: std/debug/silent/vb+/vbot")
	roomNameFlag := flag.String("room", "", "room name, e.g. 'test7'")
	vbotsFlag := flag.Int("n", 3, "number of vbots (default is 3)")
	flag.Parse()

	if *roomNameFlag == "" {
		log.Fatalln("Missing -room flag")
	}

	if *serverIPFlag == "" || *clientIPFlag == "" {
		log.Fatalln("Missing -server-ip or -client-ip flags")
	}

	if *vbotsFlag < 1 {
		log.Fatalln("-n flag must be greater than 0")
	}

	roomName := *roomNameFlag
	serverIP := *serverIPFlag
	clientIP := *clientIPFlag
	numVbots := *vbotsFlag

	room := muc.NewRandomMUC(1, numVbots)
	room.Name = roomName
	room.StreamerNames = []string{"vid-streamer-1"}

	log.Printf("Joining conference: %v", room)

	silentLogs := *loggerFlag == "silent"
	debugLogs := *loggerFlag == "split" || *loggerFlag == "debug" || *loggerFlag == "dbg"
	dumpPackets := *loggerFlag == "vb+"
	perVBotLogs := *loggerFlag == "vbot" || *loggerFlag == "bot" || *loggerFlag == "vb+"

	if !silentLogs && !debugLogs && !perVBotLogs && *loggerFlag != "std" {
		log.Println("[main] WARN unknown log mode, will default to std")
	}

	room.JoinSlowly(func(confName, viewerName string) {
		var aof *aofconf.Config

		if silentLogs {
			aof = aofconf.NewNilClientConfig()
		} else if debugLogs {
			// packet dumps and logs written in multiple files in a folder per vbot
			aof = aofconf.NewDebugLogsClientConfig(serverIP, clientIP, confName, viewerName)
		} else if perVBotLogs {
			// logs written to single file per vbot
			aof = aofconf.NewPerVBotLogsClientConfig(serverIP, clientIP, confName, viewerName, dumpPackets)
		} else {
			// all logs go to stdout/stderr
			aof = aofconf.NewStdClientConfig(serverIP, clientIP, confName, viewerName)
		}

		bosh.PerformHandshake(aof, serverIP, clientIP, confName, viewerName)
	})
}
