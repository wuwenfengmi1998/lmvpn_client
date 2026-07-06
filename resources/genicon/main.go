// genicon generates a simple 1024x1024 PNG application icon for LMVPN.
// Run: go run resources/genicon/main.go
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
)

const size = 1024

func main() {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Background: dark blue gradient.
	bg := color.RGBA{R: 30, G: 60, B: 120, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	// Draw a lighter blue circle in the center.
	cx, cy := size/2, size/2
	radius := size / 3
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x - cx)
			dy := float64(y - cy)
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist < float64(radius) {
				t := dist / float64(radius)
				r := uint8(50 + float64(100)*(1-t))
				g := uint8(100 + float64(100)*(1-t))
				b := uint8(200 + float64(55)*(1-t))
				img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}

	// Draw a white shield outline (simplified).
	shieldColor := color.RGBA{R: 255, G: 255, B: 255, A: 230}
	sx, sy := size/2, size/4
	sw, sh := size/3, size/2
	for y := sy; y < sy+sh; y++ {
		for x := sx - sw/2; x < sx+sw/2; x++ {
			// Shield shape: rounded top, pointed bottom.
			progress := float64(y-sy) / float64(sh)
			halfW := float64(sw)/2 * (1.0 - 0.3*progress*progress)
			if math.Abs(float64(x-sx)) < halfW && progress < 0.7 {
				img.SetRGBA(x, y, shieldColor)
			} else if progress >= 0.7 {
				tp := (progress - 0.7) / 0.3
				halfW2 := float64(sw)/2 * (1.0 - 0.3*0.49) * (1.0 - tp)
				if math.Abs(float64(x-sx)) < halfW2 {
					img.SetRGBA(x, y, shieldColor)
				}
			}
		}
	}

	f, err := os.Create("resources/icon.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	println("Generated resources/icon.png")
}
