package cli

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"call.zip/pkg/call"
)

const (
	defaultServerIP = "127.0.0.1"
	defaultClientIP = "127.0.0.1"
	defaultLogMode  = "std"
)

func Main() {
	serverIP := flag.String("server-ip", defaultServerIP, "conference server LAN IP")
	clientIP := flag.String("client-ip", defaultClientIP, "this machines LAN IP")
	logMode := flag.String("log", defaultLogMode, "how to log: std/debug/silent/vb+/vbot")
	roomName := flag.String("room", "", "room name, e.g. 'test7'")
	vbots := flag.Int("n", 3, "number of vbots (default is 3)")
	instantReplay := flag.Bool("preview-instant-replay", false, "preview feature")
	viewersAlwaysRetry := flag.Bool("viewers-always-retry", false, "viewers always retry when signaling fails")
	signalingConcurrency := flag.Int("signaling-concurrency", 1, "concurrency limit during signaling")
	flag.Parse()

	if *roomName == "" {
		log.Fatalln("Missing -room flag")
	}

	if *serverIP == "" || *clientIP == "" {
		log.Fatalln("Missing -server-ip or -client-ip flags")
	}

	if *vbots < 1 {
		log.Fatalln("-n flag must be greater than 0")
	}

	client := call.NewClient(call.ClientConfig{
		LogMode:  *logMode,
		ServerIP: *serverIP,
		ClientIP: *clientIP,
	})

	if *viewersAlwaysRetry {
		nViewers := *vbots
		var errs []call.WrappedSignalingError
		for {
			errs = client.JoinRoom(call.JoinRoomConfig{
				RoomName:   *roomName,
				ViewerBots: nViewers,
				ReplayBots: 0,
				Signaling: call.SignalingConfig{
					Concurrency: *signalingConcurrency,
				},
			})
			if len(errs) == 0 {
				log.Println("[viewers-always-retry] Finished joining room", *roomName)
				break
			}
			log.Printf("[viewers-always-retry] %d errors occurred, will retry in 1 second\n", len(errs))
			nViewers = len(errs)
			time.Sleep(1 * time.Second)
		}
	} else {
		if errs := client.JoinRoom(call.JoinRoomConfig{
			RoomName:   *roomName,
			ViewerBots: map[bool]int{false: *vbots, true: (*vbots) - 1}[*instantReplay],
			ReplayBots: map[bool]int{false: 0, true: 1}[*instantReplay],
			Signaling: call.SignalingConfig{
				Concurrency: *signalingConcurrency,
			},
		}); errs != nil {
			log.Printf("[cli-main] %d errors from client.JoinRoom: %v", len(errs), errs)
		}
	}

	log.Println("[cli-main] joined room", *roomName, "— press Ctrl+C to exit")

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	log.Println("[cli-main] signal received, shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Shutdown(shutdownCtx); err != nil {
		log.Println("[cli-main] shutdown error:", err)
	} else {
		log.Println("[cli-main] shutdown complete")
	}
}
