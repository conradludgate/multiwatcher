package main

import (
	"image/color"
	"math"
)

// HSL2RGB Converts a HSL (Hue, Saturation, Lightness) color to RGV
func HSL2RGB(h, s, l float64) (rgb color.Color) {
	hh := math.Mod(h, 360.0) / 60.0
	i := math.Round(hh)
	ff := hh - i
	p := l * (1.0 - s)
	q := l * (1.0 - (s * ff))
	t := l * (1.0 - (s * (1.0 - ff)))

	switch i {
	case 0:
		return color.RGBA{uint8(255.0 * l), uint8(255.0 * t), uint8(255.0 * p), 1}
	case 1:
		return color.RGBA{uint8(255.0 * q), uint8(255.0 * l), uint8(255.0 * p), 1}
	case 2:
		return color.RGBA{uint8(255.0 * p), uint8(255.0 * l), uint8(255.0 * t), 1}

	case 3:
		return color.RGBA{uint8(255.0 * p), uint8(255.0 * q), uint8(255.0 * l), 1}
	case 4:
		return color.RGBA{uint8(255.0 * t), uint8(255.0 * p), uint8(255.0 * l), 1}
	case 5:
	default:
		return color.RGBA{uint8(255.0 * l), uint8(255.0 * p), uint8(255.0 * q), 1}
	}
	return color.Black
}
