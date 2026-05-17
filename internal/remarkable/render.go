package remarkable

import (
	"image"
	"image/color"
	"math"

	"github.com/mtlgro/remark-sync/internal/remarkable/rmparse"
)

// RenderStrokes draws all strokes onto a white canvas of Remarkable's native
// dimensions (1404 × 1872) and returns the resulting image.
func RenderStrokes(strokes []rmparse.Stroke) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, rmparse.CanvasWidth, rmparse.CanvasHeight))

	// Flood-fill white background.
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetRGBA(x, y, white)
		}
	}

	for _, s := range strokes {
		if !s.IsVisible() || len(s.Points) == 0 {
			continue
		}
		col := strokeColor(s.Color)
		drawStroke(img, s, col)
	}
	return img
}

// strokeColor maps a Remarkable color ID to an RGBA value.
func strokeColor(colorID int) color.RGBA {
	switch colorID {
	case rmparse.ColorGray:
		return color.RGBA{R: 150, G: 150, B: 150, A: 255}
	default: // black and all unknown colours
		return color.RGBA{A: 255}
	}
}

// drawStroke renders a single stroke as a series of connected thick segments.
func drawStroke(img *image.RGBA, s rmparse.Stroke, col color.RGBA) {
	for i, pt := range s.Points {
		r := pointRadius(s, pt)

		stamp(img, int(pt.X), int(pt.Y), r, col)

		if i > 0 {
			prev := s.Points[i-1]
			thickLine(img, prev.X, prev.Y, pt.X, pt.Y, r, col)
		}
	}
}

// pointRadius returns the drawing radius (in pixels) for a point.
//
// v5 strokes store a base width (BrushSize) and a per-point Width float that
// directly represents rendered canvas units.  v6 strokes store Width as a
// raw uint16 (max 65535) and a Pressure 0–1 float.
func pointRadius(s rmparse.Stroke, pt rmparse.Point) float64 {
	const minRadius = 0.5

	if pt.Width > 1.0 && pt.Width < 500 {
		// v5: Width is already in canvas-unit pixels.
		r := float64(pt.Width) * 0.5 * (0.5 + float64(pt.Pressure)*0.5)
		if r < minRadius {
			r = minRadius
		}
		return r
	}

	if pt.Width >= 500 {
		// v6: Width is a uint16 (0–65535); scale to ~0.5–4 px.
		r := float64(pt.Width)/65535.0*3.5 + 0.5
		return r * (0.6 + float64(pt.Pressure)*0.4)
	}

	// Fallback: use the stroke's base width.
	r := float64(s.Width) * 0.5
	if r < minRadius {
		r = minRadius
	}
	return r
}

// stamp draws a filled circle centred on (cx, cy) with radius r.
func stamp(img *image.RGBA, cx, cy int, r float64, col color.RGBA) {
	ri := int(math.Ceil(r))
	r2 := r * r
	bounds := img.Bounds()

	for dy := -ri; dy <= ri; dy++ {
		for dx := -ri; dx <= ri; dx++ {
			if float64(dx*dx+dy*dy) > r2 {
				continue
			}
			px, py := cx+dx, cy+dy
			if px >= bounds.Min.X && px < bounds.Max.X &&
				py >= bounds.Min.Y && py < bounds.Max.Y {
				img.SetRGBA(px, py, col)
			}
		}
	}
}

// thickLine draws a filled cylinder between two points by stamping circles
// along the line at 1-pixel intervals.
func thickLine(img *image.RGBA, x0, y0, x1, y1 float32, r float64, col color.RGBA) {
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1 {
		return
	}
	steps := int(dist) + 1
	for i := 1; i <= steps; i++ {
		t := float32(i) / float32(steps)
		stamp(img, int(x0+t*float32(x1-x0)), int(y0+t*float32(y1-y0)), r, col)
	}
}
