// Package appicon draws the RTCBench logo, so the desktop builds need no image files.
package appicon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// bars are the logo's rounded bars from docs/media/logo_icon.svg, on a 32-unit square
var bars = []struct {
	width float64
	color color.RGBA
}{
	{23, color.RGBA{0xdc, 0x35, 0x45, 0xff}},
	{17, color.RGBA{0x2b, 0x78, 0xe4, 0xff}},
	{28, color.RGBA{0xf5, 0xc5, 0x18, 0xff}},
	{13, color.RGBA{0xf2, 0x8c, 0x28, 0xff}},
	{25.5, color.RGBA{0x2a, 0xb6, 0x73, 0xff}},
}

// samples is how many points per pixel side are tested, for smooth edges
const samples = 8

// Image draws the logo as a size by size image on a transparent background
func Image(size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	unit := float64(size) / 32
	for y := range size {
		for x := range size {
			var r, g, b, a int
			for sy := range samples {
				for sx := range samples {
					c, ok := barAt(
						(float64(x)+(float64(sx)+0.5)/samples)/unit,
						(float64(y)+(float64(sy)+0.5)/samples)/unit,
					)
					if ok {
						r, g, b, a = r+int(c.R), g+int(c.G), b+int(c.B), a+255
					}
				}
			}
			n := samples * samples
			out.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), uint8(a / n)})
		}
	}
	return out
}

// PNG encodes Image(size)
func PNG(size int) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, Image(size))
	return buf.Bytes(), err
}

func barAt(x, y float64) (color.RGBA, bool) {
	for i, bar := range bars {
		if inRoundedRect(x, y, 2, 2+6*float64(i), bar.width, 4, 1) {
			return bar.color, true
		}
	}
	return color.RGBA{}, false
}

func inRoundedRect(x, y, left, top, width, height, radius float64) bool {
	if x < left || x > left+width || y < top || y > top+height {
		return false
	}
	dx := math.Max(0, math.Max(left+radius-x, x-(left+width-radius)))
	dy := math.Max(0, math.Max(top+radius-y, y-(top+height-radius)))
	return dx*dx+dy*dy <= radius*radius
}
