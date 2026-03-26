package ivf

// SVCResult holds the resolved SVC mode and config for a sender.
type SVCResult struct {
	Enabled bool
	Config  SVCConfig
}

// ResolveSVC determines whether to use SVC mode and builds the SVCConfig.
//
//   - mode "auto": probe the first IVF file. SVC content -> enabled, else disabled.
//   - mode "on": force SVC. overrideSL/overrideTL override auto-detection (0 = auto).
//   - mode "off": force simple (non-SVC) path.
func ResolveSVC(cameraPaths []string, mode string, overrideSL, overrideTL, targetBitrateBps int) SVCResult {
	switch mode {
	case "off":
		return SVCResult{}
	case "on":
		sl := overrideSL
		tl := overrideTL
		if sl <= 0 || tl <= 0 {
			// Auto-detect what we can from the IVF file.
			if len(cameraPaths) > 0 {
				if n, err := ProbeSVCLayers(cameraPaths[0]); err == nil && n > 1 {
					if sl <= 0 {
						sl = n
					}
					if tl <= 0 {
						tl = 3
					}
				}
			}
			if sl <= 0 {
				sl = 3
			}
			if tl <= 0 {
				tl = 3
			}
		}
		return SVCResult{Enabled: true, Config: buildSVCConfig(sl, tl, targetBitrateBps)}
	default: // "auto"
		numSL := 1
		if len(cameraPaths) > 0 {
			if n, err := ProbeSVCLayers(cameraPaths[0]); err == nil && n > 1 {
				numSL = n
			}
		}
		if numSL <= 1 {
			return SVCResult{}
		}
		return SVCResult{Enabled: true, Config: buildSVCConfig(numSL, 3, targetBitrateBps)}
	}
}

func buildSVCConfig(numSL, numTL, targetBitrateBps int) SVCConfig {
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

// ProbeAndBuildSVCConfig is a convenience wrapper for ResolveSVC with mode="auto".
// Deprecated: use ResolveSVC for full control.
func ProbeAndBuildSVCConfig(paths []string, targetBitrateBps int) SVCConfig {
	r := ResolveSVC(paths, "auto", 0, 0, targetBitrateBps)
	if !r.Enabled {
		return buildSVCConfig(1, 1, targetBitrateBps)
	}
	return r.Config
}
