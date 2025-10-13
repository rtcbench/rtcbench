package cli

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"call.zip/pkg/call"
)

const (
	defaultServerIP = "127.0.0.1"
	defaultClientIP = "127.0.0.1"
	defaultLogMode  = "std"
	defaultNRooms   = 1
	defaultNViewers = 3
)

var (
	serverIP = flag.String("server-ip", defaultServerIP, "conference server LAN IP")
	clientIP = flag.String("client-ip", defaultClientIP, "this machines LAN IP")
	logMode  = flag.String("log", defaultLogMode, "how to log: std/debug/silent/vb+/vbot")
	roomName = flag.String("room", "", "room name, e.g. 'test7'")
	nRooms   = flag.Int("rooms", defaultNRooms, "number of rooms to join (default is 1)")
	nViewers = flag.Int("n", defaultNViewers, "number of viewers per room (default is 3)")

	// signaling related
	viewersAlwaysRetry   = flag.Bool("viewers-always-retry", false, "viewers always retry when signaling fails")
	signalingConcurrency = flag.Int("signaling-concurrency", 1, "concurrency limit during signaling")

	// preview features
	instantReplay = flag.Bool("preview-instant-replay", false, "preview feature (currently broken)")
)

func Main() {
	flag.Parse()
	checkArgs()

	client := call.NewClient(call.ClientConfig{
		LogMode:  *logMode,
		ServerIP: *serverIP,
		ClientIP: *clientIP,
	})

	joinAllRooms(client)

	log.Println("[cli-main] finished joining rooms")

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

func checkArgs() {
	if *roomName == "" {
		log.Fatalln("Missing -room flag")
	}

	if *serverIP == "" || *clientIP == "" {
		log.Fatalln("Missing -server-ip or -client-ip flags")
	}

	if *nViewers < 1 {
		log.Fatalln("-n flag must be greater than 0")
	}
}

func joinAllRooms(client *call.Client) {
	wg := sync.WaitGroup{}
	for i := 0; i < *nRooms; i++ {
		wg.Add(1)
		fmtRoomName := *roomName
		if *nRooms > 1 {
			fmtRoomName += "_" + strconv.Itoa(i)
		}
		go func() {
			log.Printf("[cli-main] joining room %q (%d viewers)", fmtRoomName, nViewers)
			joinRoom(client, fmtRoomName)
			log.Printf("[cli-main] finished joining room %q", fmtRoomName)
			wg.Done()
		}()
	}
	wg.Wait()
}

func joinRoom(client *call.Client, roomName string) {
	if *viewersAlwaysRetry {
		nViewersRemaining := *nViewers
		var errs []call.WrappedSignalingError
		for {
			errs = client.JoinRoom(call.JoinRoomConfig{
				RoomName:   roomName,
				ViewerBots: nViewersRemaining,
				ReplayBots: 0,
				Signaling: call.SignalingConfig{
					Concurrency: *signalingConcurrency,
				},
			})
			if len(errs) == 0 {
				log.Println("[viewers-always-retry] Finished joining room", roomName)
				break
			}
			log.Printf("[viewers-always-retry] %d errors occurred, will retry in 1 second\n", len(errs))
			nViewersRemaining = len(errs)
			time.Sleep(1 * time.Second)
		}
	} else {
		if errs := client.JoinRoom(call.JoinRoomConfig{
			RoomName:   roomName,
			ViewerBots: map[bool]int{false: *nViewers, true: (*nViewers) - 1}[*instantReplay],
			ReplayBots: map[bool]int{false: 0, true: 1}[*instantReplay],
			Signaling: call.SignalingConfig{
				Concurrency: *signalingConcurrency,
			},
		}); errs != nil {
			log.Printf("[cli-main] %d errors from client.JoinRoom: %v", len(errs), errs)
		}
	}
}
