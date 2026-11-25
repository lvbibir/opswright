//go:build ignore

// gen renders the app icon from icon/logo.jpg. Run it from the repository
// root after changing the logo:
//
//	go run icon/gen.go
//
// It writes resources/icon.png, a rounded square nearly edge to edge, which
// mygo build turns into the Windows icons; resources/icon-dev.png, the same
// grid for mygo dev, which does not ship the dev icon; and icon/logo-ui.png,
// the logo the interface draws, which would take 80MB to decode from the
// 4096px source at every launch.
package main

import (
	"image"
	stddraw "image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"os"

	"golang.org/x/image/draw"
)

const size = 1024

// uiSize is the side of icon/logo-ui.png, as uiSize in logo.go.
const uiSize = 256

func main() {
	f, err := os.Open("icon/logo.jpg")
	if err != nil {
		log.Fatal(err)
	}
	logo, err := jpeg.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	if err := writePNG("resources/icon.png", rounded(logo, 40, false)); err != nil {
		log.Fatal(err)
	}
	mac := rounded(logo, 100, true)
	if err := writePNG("resources/icon-dev.png", mac); err != nil {
		log.Fatal(err)
	}
	small := image.NewRGBA(image.Rect(0, 0, uiSize, uiSize))
	draw.CatmullRom.Scale(small, small.Bounds(), logo, logo.Bounds(), draw.Src, nil)
	if err := writePNG("icon/logo-ui.png", small); err != nil {
		log.Fatal(err)
	}
}

// rounded draws logo into a superellipse (continuous corners) inset
// px from each edge of a size×size canvas, with a drop shadow when
// shadow is set.
func rounded(logo image.Image, inset int, shadow bool) *image.RGBA {
	s := size - 2*inset
	art := image.NewNRGBA(image.Rect(0, 0, s, s))
	draw.CatmullRom.Scale(art, art.Bounds(), logo, logo.Bounds(), draw.Src, nil)

	// Coverage of the superellipse |x|^5 + |y|^5 <= 1, 4×4 samples a pixel.
	const n, ss = 5.0, 4
	pow := make([]float64, s*ss)
	for i := range pow {
		pow[i] = math.Pow(math.Abs((float64(i)+0.5)/float64(s*ss)*2-1), n)
	}
	mask := make([]float64, s*s)
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			in := 0
			for j := 0; j < ss; j++ {
				for i := 0; i < ss; i++ {
					if pow[x*ss+i]+pow[y*ss+j] <= 1 {
						in++
					}
				}
			}
			mask[y*s+x] = float64(in) / ss / ss
			art.Pix[(y*s+x)*4+3] = uint8(math.Round(mask[y*s+x] * 255))
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	if shadow {
		// A black shadow at 30%, 10px down, blurred 10px.
		a := make([]float64, size*size)
		for y := 0; y < s; y++ {
			copy(a[(y+inset+10)*size+inset:], mask[y*s:(y+1)*s])
		}
		for range 3 {
			blur(a, 5)
		}
		for i, v := range a {
			dst.Pix[i*4+3] = uint8(math.Round(v * 0.3 * 255))
		}
	}
	stddraw.Draw(dst, art.Bounds().Add(image.Pt(inset, inset)), art, image.Point{}, stddraw.Over)
	return dst
}

// blur is one box blur of radius r over the size×size plane a, across and
// then down.
func blur(a []float64, r int) {
	line := make([]float64, size)
	for pass := 0; pass < 2; pass++ {
		for k := 0; k < size; k++ {
			at := func(i int) *float64 {
				if pass == 0 {
					return &a[k*size+i]
				}
				return &a[i*size+k]
			}
			sum := 0.0
			for i := -r; i <= r; i++ {
				if i >= 0 && i < size {
					sum += *at(i)
				}
			}
			for i := 0; i < size; i++ {
				line[i] = sum / float64(2*r+1)
				if j := i - r; j >= 0 {
					sum -= *at(j)
				}
				if j := i + r + 1; j < size {
					sum += *at(j)
				}
			}
			for i := 0; i < size; i++ {
				*at(i) = line[i]
			}
		}
	}
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
