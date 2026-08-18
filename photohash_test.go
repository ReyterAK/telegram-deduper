package main

import (
	"image"
	"image/color"
	"math/rand"
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
	rng := rand.New(rand.NewSource(seed))
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetGray(x, y, color.Gray{Y: uint8(rng.Intn(256))})
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
