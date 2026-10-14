// Command winicon writes the icon and manifest resources that go build links into
// rtcbench.exe. make desktop runs it on Windows; the .syso files it writes are not committed.
package main

import (
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"

	"github.com/rtcbench/rtcbench/internal/appicon"
	"github.com/tc-hib/winres"
)

func main() {
	dir := "cmd/rtcbench"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	var images []image.Image
	for _, size := range []int{16, 20, 24, 32, 40, 48, 64, 128, 256} {
		images = append(images, appicon.Image(size))
	}
	icon, err := winres.NewIconFromImages(images)
	if err != nil {
		log.Fatal(err)
	}
	rs := winres.ResourceSet{}
	// Wails loads the window icon from resource ID 3
	if err := rs.SetIcon(winres.ID(3), icon); err != nil {
		log.Fatal(err)
	}
	rs.SetManifest(winres.AppManifest{DPIAwareness: winres.DPIPerMonitorV2, UseCommonControlsV6: true})
	for _, arch := range []winres.Arch{winres.ArchAMD64, winres.ArchARM64} {
		path := filepath.Join(dir, fmt.Sprintf("rsrc_windows_%s.syso", arch))
		f, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := rs.WriteObject(f, arch); err != nil {
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
	}
}
