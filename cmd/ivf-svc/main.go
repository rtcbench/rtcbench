// ivf-svc repackages a single-layer VP9 IVF file into a multi-spatial-layer
// superframe IVF suitable for SVC testing.
//
// Usage: ivf-svc -layers 3 input.ivf output.ivf
package main

import (
	"flag"
	"fmt"
	"os"

	"call.zip/pkg/ivf"
)

func main() {
	layers := flag.Int("layers", 3, "number of spatial layers (1-3)")
	flag.Parse()

	if flag.NArg() != 2 {
		fmt.Fprintf(os.Stderr, "usage: ivf-svc -layers N input.ivf output.ivf\n")
		os.Exit(1)
	}

	src := flag.Arg(0)
	dst := flag.Arg(1)

	if err := ivf.RepackageSVCIVF(src, dst, *layers); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d-layer SVC IVF: %s\n", *layers, dst)
}
