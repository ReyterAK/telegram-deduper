//
// photohash.go
// Antidubl — perceptual image fingerprinting (dHash)
//
// A 64-bit difference hash: the photo is downscaled to 9x8
// grayscale; each bit records whether the pixel is brighter than
// its left neighbour. Re-uploads, resizes and recompressions of
// the same picture yield a nearly identical hash; two genuinely
// different pictures are far apart in Hamming distance.
//

package main

import (
	"fmt"
	"image"
	"math/bits"
	"net/http"

	// Register JPEG/PNG/GIF decoders; without these blank imports
	// image.Decode fails with "image: unknown format".
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// PhotoHashThreshold is the max Hamming distance (of 64 bits) for
// two images to count as the same picture.
const PhotoHashThreshold = 10

// dHash computes the 64-bit difference hash of an image.
func dHash(img image.Image) uint64 {
	dst := image.NewGray(image.Rect(0, 0, 9, 8))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	var h uint64
	for y := range 8 {
		for x := range 8 {
			if dst.GrayAt(x+1, y).Y > dst.GrayAt(x, y).Y {
				h |= 1 << uint(y*8+x)
			}
		}
	}
	return h
}

func hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// hashFromURL downloads an image and computes its dHash hex.
func hashFromURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%016x", dHash(img)), nil
}

// photoHash downloads the largest size of a photo message and
// returns its dHash hex.
func (d *Detector) photoHash(m *tgbotapi.Message) (string, error) {
	if len(m.Photo) == 0 {
		return "", fmt.Errorf("no photo")
	}
	photo := m.Photo[len(m.Photo)-1]
	file, err := d.bot.GetFile(tgbotapi.FileConfig{FileID: photo.FileID})
	if err != nil {
		return "", err
	}
	return hashFromURL(file.Link(d.bot.Token))
}
