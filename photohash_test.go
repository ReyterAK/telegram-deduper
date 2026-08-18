package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"testing"
)

func gradientImage(size int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetGray(x, y, color.Gray{Y: uint8(x * 255 / size)})
		}
	}
	return img
}

func gradientImageBrightness(size, delta int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			v := x*255/size + delta
			if v > 255 {
				v = 255
			}
			img.SetGray(x, y, color.Gray{Y: uint8(v)})
		}
	}
	return img
}

func solidImage(v uint8) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	return img
}

func noiseImage(size int, seed int64) *image.Gray {
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetGray(x, y, color.Gray{Y: uint8(rng.IntN(256))})
		}
	}
	return img
}

func TestDHashDeterministicAndSolid(t *testing.T) {
	g := gradientImage(64)
	if dHash(g) != dHash(g) {
		t.Fatal("dHash must be deterministic")
	}
	if dHash(solidImage(50)) != 0 {
		t.Fatal("solid-color image must hash to 0 (no differences)")
	}
}

func TestDHashNearDuplicates(t *testing.T) {
	g1 := gradientImage(64)
	// same pattern, slightly brighter → same relative differences
	g2 := gradientImageBrightness(64, 1)
	if d := hamming(dHash(g1), dHash(g2)); d > 2 {
		t.Fatalf("brightness shift must be near-identical, distance=%d", d)
	}
	// same pattern at a different resolution → near-identical
	small := gradientImage(32)
	if d := hamming(dHash(g1), dHash(small)); d > 4 {
		t.Fatalf("resize must be near-identical, distance=%d", d)
	}
}

func TestDHashDifferentImages(t *testing.T) {
	g := dHash(gradientImage(64))
	s := dHash(solidImage(128))
	n := dHash(noiseImage(64, 42))
	if hamming(g, s) <= PhotoHashThreshold {
		t.Fatal("gradient and solid must differ")
	}
	if hamming(g, n) <= PhotoHashThreshold {
		t.Fatal("gradient and noise must differ")
	}
	if hamming(s, n) <= PhotoHashThreshold {
		t.Fatal("solid and noise must differ")
	}
}

func TestDistinctiveHash(t *testing.T) {
	// near-uniform images (solid colour, blank pages, black
	// openings) are not distinctive: their hash would collide with
	// unrelated uniform images, so they must not be matched.
	if _, ok := distinctiveHash(solidImage(50)); ok {
		t.Fatal("solid image must not be distinctive")
	}
	if _, ok := distinctiveHash(solidImage(0)); ok {
		t.Fatal("black image must not be distinctive")
	}
	if _, ok := distinctiveHash(gradientImage(64)); !ok {
		t.Fatal("gradient must be distinctive")
	}
	if _, ok := distinctiveHash(noiseImage(64, 7)); !ok {
		t.Fatal("noise must be distinctive")
	}
}

func TestImageDecodeRegistered(t *testing.T) {
	// Regression: image.Decode must know PNG/JPEG/GIF (blank imports).
	var buf bytes.Buffer
	img := gradientImage(32)
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(&buf)
	if err != nil {
		t.Fatalf("decoders not registered: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q", format)
	}
	if hamming(dHash(decoded), dHash(img)) != 0 {
		t.Fatal("decode must preserve the perceptual hash")
	}
}

func TestHamming(t *testing.T) {
	if hamming(0, 0) != 0 {
		t.Fatal("identical hashes must have 0 distance")
	}
	if hamming(0, 1) != 1 {
		t.Fatal("one-bit distance expected")
	}
	if hamming(0, ^uint64(0)) != 64 {
		t.Fatal("all-bits distance expected")
	}
}
