// ivf-svc repackages VP9 IVF files into a multi-spatial-layer superframe IVF
// suitable for SVC testing.
//
// Two modes:
//
//	Duplicate mode (fake SVC — all layers are identical):
//	  ivf-svc -layers N input.ivf output.ivf
//
//	Multi-layer mode (real per-layer bitrates — preferred):
//	  ivf-svc s0.ivf s1.ivf s2.ivf output.ivf
//
// In multi-layer mode the inputs are the spatial layers lowest-to-highest
// (S0 first). Each input IVF should be independently encoded at the
// resolution and bitrate appropriate for that layer. The output IVF uses the
// dimensions of the highest (last) input and packages one frame per layer
// into each VP9 superframe.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rtcbench/rtcbench/pkg/ivf"
)

func main() {
	layers := flag.Int("layers", 0, "duplicate mode: number of spatial layers (1-3)")
	flag.Parse()

	if *layers > 0 {
		// Duplicate mode: single input, N identical layers
		if flag.NArg() != 2 {
			fmt.Fprintf(os.Stderr, "usage: ivf-svc -layers N input.ivf output.ivf\n")
			os.Exit(1)
		}
		src, dst := flag.Arg(0), flag.Arg(1)
		if err := ivf.RepackageSVCIVF(src, dst, *layers); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %d-layer SVC IVF (duplicate mode): %s\n", *layers, dst)
		return
	}

	// Multi-layer mode: N input IVFs + 1 output IVF
	args := flag.Args()
	if len(args) < 2 || len(args) > 4 {
		fmt.Fprintf(os.Stderr, "usage:\n")
		fmt.Fprintf(os.Stderr, "  ivf-svc -layers N input.ivf output.ivf         (duplicate)\n")
		fmt.Fprintf(os.Stderr, "  ivf-svc s0.ivf [s1.ivf [s2.ivf]] output.ivf   (multi-layer)\n")
		os.Exit(1)
	}
	srcPaths := args[:len(args)-1]
	dstPath := args[len(args)-1]
	if err := ivf.RepackageSVCIVFMulti(srcPaths, dstPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d-layer SVC IVF: %s\n", len(srcPaths), dstPath)
}
