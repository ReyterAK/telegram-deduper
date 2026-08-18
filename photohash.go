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

// MediaThumbThreshold is the stricter bound for video/document
// thumbnails: different videos often share similar first frames
// (black openings, logos), so the match must be tighter.
const MediaThumbThreshold = 6

// MinDistinctiveBits is the minimum number of set bits a hash must
// have to be usable: a (near-)uniform image — solid colour, a
// blank page, a black opening frame — hashes to ~0 and would match
// every other uniform image. Such hashes are treated as
// non-distinctive and never matched perceptually.
const MinDistinctiveBits = 2

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

// distinctiveHash returns the dHash and true when the image is
// distinctive enough to be matched perceptually; false for
// near-uniform images whose hash would collide with unrelated ones.
func distinctiveHash(img image.Image) (uint64, bool) {
	h := dHash(img)
	return h, hamming(h, 0) > MinDistinctiveBits
}

func hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// hashFromURL downloads an image and computes its dHash hex.
// Returns ("", nil) for images that are not distinctive enough.
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
	h, ok := distinctiveHash(img)
	if !ok {
		return "", nil // uniform image — not a reliable fingerprint
	}
	return fmt.Sprintf("%016x", h), nil
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

// thumbHash downloads a Telegram thumbnail (video/document) and
// returns its dHash hex.
func (d *Detector) thumbHash(ps *tgbotapi.PhotoSize) (string, error) {
	if ps == nil {
		return "", fmt.Errorf("no thumbnail")
	}
	file, err := d.bot.GetFile(tgbotapi.FileConfig{FileID: ps.FileID})
	if err != nil {
		return "", err
	}
	return hashFromURL(file.Link(d.bot.Token))
}

// mediaHash returns the perceptual hash for a media message:
// the photo itself, or the thumbnail for videos and documents.
// Returns ("", nil) for media without a hashable preview.
func (d *Detector) mediaHash(m *tgbotapi.Message, mediaType string) (string, error) {
	switch mediaType {
	case MediaTypePhoto:
		return d.photoHash(m)
	case MediaTypeVideo:
		if m.Video == nil {
			return "", fmt.Errorf("no video")
		}
		return d.thumbHash(m.Video.Thumbnail)
	case MediaTypeDocument:
		if m.Document == nil {
			return "", fmt.Errorf("no document")
		}
		return d.thumbHash(m.Document.Thumbnail)
	}
	return "", fmt.Errorf("unsupported media type %q", mediaType)
}
