package appicon

import (
	"bytes"
	"image/png"
	"testing"
)

func TestImageDrawsTheBarsOnATransparentSquare(t *testing.T) {
	img := Image(32)
	if got := img.Bounds().Dx(); got != 32 {
		t.Fatalf("width = %d, want 32", got)
	}
	// The corner is outside every bar, and the middle of the first bar is solid red
	if c := img.RGBAAt(0, 0); c.A != 0 {
		t.Errorf("corner = %v, want transparent", c)
	}
	if c := img.RGBAAt(10, 3); c.R != 0xdc || c.A != 0xff {
		t.Errorf("first bar = %v, want opaque #dc3545", c)
	}
	// The yellow bar is the widest, ending 2 units from the right edge
	if c := img.RGBAAt(28, 15); c.R != 0xf5 || c.A != 0xff {
		t.Errorf("third bar = %v, want opaque #f5c518", c)
	}
	if c := img.RGBAAt(31, 15); c.A != 0 {
		t.Errorf("right margin = %v, want transparent", c)
	}
}

func TestPNGDecodes(t *testing.T) {
	data, err := PNG(256)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Dx(); got != 256 {
		t.Fatalf("width = %d, want 256", got)
	}
}
