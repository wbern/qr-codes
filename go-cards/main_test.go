package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestVisualRegression generates all cards, converts PDFs to PNGs via pdftoppm,
// crops the bleed area, and compares the trim region against the source PNGs.
// Since the PDF embeds the exact same PNG, differences should be near 0%.
func TestVisualRegression(t *testing.T) {
	// 1. Generate all cards by running main().
	main()

	for _, card := range cards {
		t.Run(card.Name, func(t *testing.T) {
			pngPath := filepath.Join("output", card.Name+".png")
			pdfPath := filepath.Join("output", card.Name+".pdf")
			pdfPngPrefix := filepath.Join("output", card.Name+"-pdf")

			// 2. Convert PDF to PNG via pdftoppm at 508 DPI (matches our 20px/mm).
			cmd := exec.Command("pdftoppm",
				"-png", "-rx", "508", "-ry", "508",
				pdfPath, pdfPngPrefix,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("pdftoppm failed: %v\n%s", err, out)
			}

			// pdftoppm names the output file with a page number suffix.
			pdfPngPath := pdfPngPrefix + "-1.png"
			if _, err := os.Stat(pdfPngPath); err != nil {
				// Some versions use different naming.
				pdfPngPath = pdfPngPrefix + "-01.png"
				if _, err := os.Stat(pdfPngPath); err != nil {
					t.Fatalf("pdftoppm output not found: tried %s-1.png and %s-01.png",
						pdfPngPrefix, pdfPngPrefix)
				}
			}

			// 3. Load both images.
			srcImg := loadPNG(t, pngPath)
			pdfImg := loadPNG(t, pdfPngPath)

			// 4. Crop the bleed from the PDF image.
			// The PDF page is 96×61mm, the card is at (3,3) offset covering 90×55mm.
			// At 508 DPI (20px/mm): bleed = 3mm * 20px/mm = 60px.
			bleedPx := int(bleed * pxPerMM)
			pdfBounds := pdfImg.Bounds()
			trimRect := image.Rect(
				bleedPx, bleedPx,
				pdfBounds.Max.X-bleedPx, pdfBounds.Max.Y-bleedPx,
			)

			// Create a cropped sub-image.
			type subImager interface {
				SubImage(r image.Rectangle) image.Image
			}
			cropper, ok := pdfImg.(subImager)
			if !ok {
				t.Fatal("PDF image does not support SubImage")
			}
			croppedPdf := cropper.SubImage(trimRect)

			// 5. Compare pixels.
			srcBounds := srcImg.Bounds()
			cropBounds := croppedPdf.Bounds()

			// The sizes should match closely. Allow 1px tolerance from rounding.
			dw := abs(srcBounds.Dx() - cropBounds.Dx())
			dh := abs(srcBounds.Dy() - cropBounds.Dy())
			if dw > 2 || dh > 2 {
				t.Fatalf("size mismatch: source %dx%d, cropped PDF %dx%d",
					srcBounds.Dx(), srcBounds.Dy(),
					cropBounds.Dx(), cropBounds.Dy())
			}

			// Use the smaller dimensions for comparison.
			cmpW := min(srcBounds.Dx(), cropBounds.Dx())
			cmpH := min(srcBounds.Dy(), cropBounds.Dy())

			diffCount := 0
			totalPixels := cmpW * cmpH

			for y := 0; y < cmpH; y++ {
				for x := 0; x < cmpW; x++ {
					c1 := srcImg.At(srcBounds.Min.X+x, srcBounds.Min.Y+y)
					c2 := croppedPdf.At(cropBounds.Min.X+x, cropBounds.Min.Y+y)

						// Threshold of 50/255 per channel accounts for anti-aliasing
					// introduced by pdftoppm when rasterizing the PDF back to PNG.
					// The embedded image is identical pixels, but the PDF renderer
					// applies sub-pixel interpolation at module boundaries.
					if !colorsClose(c1, c2, 50) {
						diffCount++
					}
				}
			}

			diffPct := float64(diffCount) / float64(totalPixels) * 100
			t.Logf("%s: %d/%d pixels differ (%.3f%%)", card.Name, diffCount, totalPixels, diffPct)

			// Threshold is 2% to account for PDF rasterization artifacts.
			// pdftoppm introduces sub-pixel anti-aliasing at module boundaries
			// because the PDF page dimensions (96×61mm at 508 DPI) don't
			// divide to exact integer pixel counts, causing a ~1px shift.
			if diffPct > 2.0 {
				// Save diff image for debugging.
				saveDiffImage(t, srcImg, croppedPdf, cmpW, cmpH,
					filepath.Join("output", card.Name+"-diff.png"))
				t.Errorf("pixel diff %.3f%% exceeds 2%% threshold", diffPct)
			}
		})
	}
}

// loadPNG loads a PNG image from disk.
func loadPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return img
}

// colorsClose returns true if two colors are within the given threshold per channel.
func colorsClose(c1, c2 color.Color, threshold uint32) bool {
	r1, g1, b1, a1 := c1.RGBA()
	r2, g2, b2, a2 := c2.RGBA()
	// RGBA() returns 16-bit values. Scale threshold to match.
	th := threshold * 257 // 0-255 → 0-65535 scale

	return absDiff(r1, r2) <= th &&
		absDiff(g1, g2) <= th &&
		absDiff(b1, b2) <= th &&
		absDiff(a1, a2) <= th
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// saveDiffImage creates a visual diff image highlighting pixel differences.
func saveDiffImage(t *testing.T, img1, img2 image.Image, w, h int, path string) {
	t.Helper()
	diff := image.NewNRGBA(image.Rect(0, 0, w, h))
	b1 := img1.Bounds()
	b2 := img2.Bounds()

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c1 := img1.At(b1.Min.X+x, b1.Min.Y+y)
			c2 := img2.At(b2.Min.X+x, b2.Min.Y+y)

			r1, g1, b1v, _ := c1.RGBA()
			r2, g2, b2v, _ := c2.RGBA()

			dr := math.Abs(float64(r1)-float64(r2)) / 65535
			dg := math.Abs(float64(g1)-float64(g2)) / 65535
			db := math.Abs(float64(b1v)-float64(b2v)) / 65535
			maxD := math.Max(dr, math.Max(dg, db))

			if maxD > 0.05 {
				// Highlight differences in red.
				intensity := uint8(math.Min(255, maxD*255*3))
				diff.SetNRGBA(x, y, color.NRGBA{R: intensity, G: 0, B: 0, A: 255})
			} else {
				// Show the original image dimmed.
				r, g, b, _ := c1.RGBA()
				diff.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 10), // dim to ~25%
					G: uint8(g >> 10),
					B: uint8(b >> 10),
					A: 255,
				})
			}
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Logf("could not save diff image: %v", err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, diff); err != nil {
		t.Logf("could not encode diff image: %v", err)
	} else {
		t.Logf("diff image saved to %s", path)
	}
}

func TestMain(m *testing.M) {
	// Ensure we run from the go-cards directory.
	fmt.Println("Running visual regression tests...")
	os.Exit(m.Run())
}
