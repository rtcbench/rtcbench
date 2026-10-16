package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
)

// openWindow is set by desktop builds; it shows url and returns when the window closes
// It must run on the main goroutine because native windows need the main thread
var openWindow func(ctx context.Context, url, profile string) error

// serveDesktop keeps data in the user config directory and a fixed local port when it can,
// so the window's saved settings persist between launches
func serveDesktop(ctx context.Context, settingsFile string, options Options) error {
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	home := filepath.Join(config, "RTCBench")
	data := filepath.Join(home, "data")
	listener, err := net.Listen("tcp", "127.0.0.1:47380")
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan string, 1)
	served := make(chan error, 1)
	go func() {
		served <- serveAPI(ctx, listener, data, settingsFile, options, func(url string) { ready <- url })
	}()
	var url string
	select {
	case err := <-served:
		return err
	case url = <-ready:
	}
	err = openWindow(ctx, url, filepath.Join(home, "webview"))
	cancel()
	return errors.Join(err, <-served)
}
