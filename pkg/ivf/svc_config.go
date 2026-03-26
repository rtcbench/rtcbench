package ivf

// ProbeAndBuildSVCConfig auto-detects SVC layer count from the first IVF file
// and builds an SVCConfig. If paths is empty or the file is not SVC, returns
// a 1x1 config (single spatial, single temporal).
func ProbeAndBuildSVCConfig(paths []string, targetBitrateBps int) SVCConfig {
	numSL := 1
	if len(paths) > 0 {
		if n, err := ProbeSVCLayers(paths[0]); err == nil && n > 0 {
			numSL = n
		}
	}
	numTL := 1
	if numSL > 1 {
		numTL = 3
	}

	cfg := SVCConfig{
		NumSpatialLayers:  numSL,
		NumTemporalLayers: numTL,
		Widths:            make([]uint16, numSL),
		Heights:           make([]uint16, numSL),
		TargetBitrateBps:  targetBitrateBps,
	}

	resolutions := [][2]uint16{{640, 360}, {1280, 720}, {1920, 1080}}
	for i := range numSL {
		ri := i + (3 - numSL)
		cfg.Widths[i] = resolutions[ri][0]
		cfg.Heights[i] = resolutions[ri][1]
	}

	return cfg
}
