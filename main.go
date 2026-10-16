package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	"golang.org/x/image/draw"
)

// diffThreshold is the sum of absolute RGB differences required to mark a pixel as changed.
// Tune this: higher = only big changes highlighted; lower = noise highlighted too.
const diffThreshold = 180

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <frame1.png> <frame2.png> <output.png>\n", os.Args[0])
		os.Exit(2)
	}

	frame1, err := loadPNG(os.Args[1])
	if err != nil {
		log.Fatalf("load frame1: %v", err)
	}
	frame2, err := loadPNG(os.Args[2])
	if err != nil {
		log.Fatalf("load frame2: %v", err)
	}

	b1 := frame1.Bounds()
	b2 := frame2.Bounds()
	if b1 != b2 {
		log.Fatalf("frames must have identical dimensions: %v vs %v", b1, b2)
	}

	n1 := toNRGBA(frame1)
	n2 := toNRGBA(frame2)

	// Binary mask: opaque where pixels changed, transparent where they didn't.
	mask := buildDiffMask(n1, n2, b1, diffThreshold)

	// Highlight color: semi-transparent red.
	highlight := image.NewUniform(color.NRGBA{R: 255, G: 0, B: 0, A: 140})

	// Destination: start from frame 2, then composite red only on changed pixels.
	dst := cloneNRGBA(n2)
	draw.DrawMask(dst, b1, highlight, image.Point{}, mask, image.Point{}, draw.Over)

	out, err := os.Create(os.Args[3])
	if err != nil {
		log.Fatalf("create output: %v", err)
	}
	defer out.Close()
	if err := png.Encode(out, dst); err != nil {
		log.Fatalf("encode output: %v", err)
	}

	// Save the raw mask so you can inspect what was flagged as changed.
	maskFile, err := os.Create("diff-mask.png")
	if err != nil {
		log.Fatalf("create mask: %v", err)
	}
	defer maskFile.Close()
	if err := png.Encode(maskFile, mask); err != nil {
		log.Fatalf("encode mask: %v", err)
	}

	fmt.Printf("wrote %s and diff-mask.png (%d changed pixels)\n", os.Args[3], changedCount(mask, b1))
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	nrgba := image.NewNRGBA(b)
	draw.Draw(nrgba, b, img, image.Point{}, draw.Src)
	return nrgba
}

func buildDiffMask(a, b *image.NRGBA, rect image.Rectangle, threshold uint32) *image.Alpha {
	mask := image.NewAlpha(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			pa := a.NRGBAAt(x, y)
			pb := b.NRGBAAt(x, y)
			d := uint32(abs(int(pa.R)-int(pb.R))) +
				uint32(abs(int(pa.G)-int(pb.G))) +
				uint32(abs(int(pa.B)-int(pb.B)))
			if d > threshold {
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			}
		}
	}
	return mask
}

func cloneNRGBA(n *image.NRGBA) *image.NRGBA {
	clone := image.NewNRGBA(n.Bounds())
	copy(clone.Pix, n.Pix)
	return clone
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func changedCount(mask *image.Alpha, rect image.Rectangle) int {
	count := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if mask.At(x, y).(color.Alpha).A > 0 {
				count++
			}
		}
	}
	return count
}
