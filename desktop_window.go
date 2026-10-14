//go:build desktop

package rtcbench

import (
	"context"
	"fmt"
	"net/http"
	"runtime"

	"github.com/rtcbench/rtcbench/internal/appicon"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// detachConsole closes the console Windows opens when the app is started by double-click
var detachConsole = func() {}

// followTabs keeps macOS's traffic lights centred on the tabs, which move with the workspace size
var followTabs = func(window context.Context) {}

func init() {
	openWindow = openWails
}

// openWails shows the app served at url in a borderless window; macOS keeps its traffic lights over the app
// The page posts Wails window messages itself because Wails only injects its runtime into pages it serves
func openWails(ctx context.Context, url, profile string) error {
	detachConsole()
	page := fmt.Sprintf("%s/?desktop=%s", url, runtime.GOOS)
	var icon []byte
	if runtime.GOOS == "linux" {
		var err error
		if icon, err = appicon.PNG(256); err != nil {
			return err
		}
	}
	done := make(chan struct{})
	defer close(done)
	return wails.Run(&options.App{
		Title:                  "RTCBench",
		Width:                  1400,
		Height:                 900,
		MinWidth:               640,
		MinHeight:              480,
		Frameless:              runtime.GOOS != "darwin",
		BackgroundColour:       &options.RGBA{R: 238, G: 238, B: 238, A: 255},
		BindingsAllowedOrigins: url,
		AssetServer: &assetserver.Options{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprintf(w, "<script>location.replace(%q)</script>", page)
			}),
		},
		OnStartup: func(window context.Context) {
			followTabs(window)
			go func() {
				select {
				case <-ctx.Done():
					wailsruntime.Quit(window)
				case <-done:
				}
			}()
		},
		Windows: &windows.Options{WebviewUserDataPath: profile},
		Mac:     &mac.Options{TitleBar: mac.TitleBarHidden()},
		Linux:   &linux.Options{ProgramName: "RTCBench", Icon: icon, WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand},
	})
}
