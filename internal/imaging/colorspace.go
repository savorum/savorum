package imaging

import "math"

// ColorSpace is what the pixel values of a decoded image mean: how each
// channel maps to linear light, and how linear RGB maps to CIE XYZ.
type ColorSpace struct {
	// toXYZ maps linear RGB to XYZ relative to D50, the ICC profile
	// connection space, so ICC and CICP descriptions share one target.
	toXYZ    matrix
	transfer [3]func(float64) float64
	dynamic  dynamicRange
	srgb     bool
}

type dynamicRange int

const (
	standardRange dynamicRange = iota
	// perceptualQuantizer is SMPTE ST 2084; linear values are nits.
	perceptualQuantizer
	// hybridLogGamma is ITU-R BT.2100 HLG; linear values are display nits.
	hybridLogGamma
)

// HDR reports whether the space carries high dynamic range values that must
// be tone-mapped before they fit an SDR output.
func (s ColorSpace) HDR() bool { return s.dynamic != standardRange }

// SRGB is the canonical output space and the assumption for an image whose
// file does not describe its color.
var SRGB = ColorSpace{toXYZ: srgbToXYZ, transfer: [3]func(float64) float64{srgbDecode, srgbDecode, srgbDecode}, srgb: true}

// ResolveColorSpace reads a file's color description. A description this
// package cannot interpret resolves to sRGB, which is what a browser shows for
// an untagged image.
func ResolveColorSpace(description ColorDescription) ColorSpace {
	switch {
	case description.CICP != nil:
		return cicpSpace(*description.CICP)
	case len(description.ICC) > 0:
		if space, ok := iccSpace(description.ICC); ok {
			return space
		}
	}

	return SRGB
}

type chromaticity struct{ x, y float64 }

type primaries struct{ red, green, blue, white chromaticity }

var (
	whiteD65 = chromaticity{0.3127, 0.3290}
	whiteC   = chromaticity{0.310, 0.316}

	bt709 = primaries{chromaticity{0.64, 0.33}, chromaticity{0.30, 0.60}, chromaticity{0.15, 0.06}, whiteD65}

	// H.273 ColourPrimaries, by code point.
	cicpPrimaries = map[uint16]primaries{
		1:  bt709,
		4:  {chromaticity{0.67, 0.33}, chromaticity{0.21, 0.71}, chromaticity{0.14, 0.08}, whiteC},
		5:  {chromaticity{0.64, 0.33}, chromaticity{0.29, 0.60}, chromaticity{0.15, 0.06}, whiteD65},
		6:  {chromaticity{0.630, 0.340}, chromaticity{0.310, 0.595}, chromaticity{0.155, 0.070}, whiteD65},
		7:  {chromaticity{0.630, 0.340}, chromaticity{0.310, 0.595}, chromaticity{0.155, 0.070}, whiteD65},
		8:  {chromaticity{0.681, 0.319}, chromaticity{0.243, 0.692}, chromaticity{0.145, 0.049}, whiteC},
		9:  {chromaticity{0.708, 0.292}, chromaticity{0.170, 0.797}, chromaticity{0.131, 0.046}, whiteD65},
		11: {chromaticity{0.680, 0.320}, chromaticity{0.265, 0.690}, chromaticity{0.150, 0.060}, chromaticity{0.314, 0.351}},
		12: {chromaticity{0.680, 0.320}, chromaticity{0.265, 0.690}, chromaticity{0.150, 0.060}, whiteD65},
		22: {chromaticity{0.630, 0.340}, chromaticity{0.295, 0.605}, chromaticity{0.155, 0.077}, whiteD65},
	}

	srgbToXYZ   = primaryMatrix(bt709)
	xyzToLinear = srgbToXYZ.inverse()
)

func cicpSpace(code CICP) ColorSpace {
	chosen, ok := cicpPrimaries[code.Primaries]
	if !ok {
		chosen = bt709
	}

	space := ColorSpace{toXYZ: primaryMatrix(chosen)}

	curve := srgbDecode

	switch code.Transfer {
	case 4:
		curve = gammaDecode(2.2)
	case 5:
		curve = gammaDecode(2.8)
	case 8:
		curve = func(value float64) float64 { return value }
	case 16:
		curve, space.dynamic = pqDecode, perceptualQuantizer
	case 18:
		curve, space.dynamic = hlgDecode, hybridLogGamma
	}
	// BT.709, BT.601, and BT.2020 SDR transfers (1, 6, 14, 15) are camera
	// encodings; images tagged with them are made to be shown like sRGB,
	// which is how browsers display them.
	space.transfer = [3]func(float64) float64{curve, curve, curve}
	space.srgb = (code.Primaries == 1 || code.Primaries == 2) && space.dynamic == standardRange && code.Transfer != 4 && code.Transfer != 5 && code.Transfer != 8

	return space
}

func srgbDecode(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}

	return math.Pow((value+0.055)/1.055, 2.4)
}

func srgbEncode(value float64) float64 {
	if value <= 0.0031308 {
		return value * 12.92
	}

	return 1.055*math.Pow(value, 1/2.4) - 0.055
}

func gammaDecode(gamma float64) func(float64) float64 {
	return func(value float64) float64 { return math.Pow(max(value, 0), gamma) }
}

// pqDecode returns absolute luminance in nits.
func pqDecode(value float64) float64 {
	const (
		m1 = 2610.0 / 16384
		m2 = 2523.0 / 4096 * 128
		c1 = 3424.0 / 4096
		c2 = 2413.0 / 4096 * 32
		c3 = 2392.0 / 4096 * 32
	)

	power := math.Pow(max(value, 0), 1/m2)

	return 10000 * math.Pow(max(power-c1, 0)/(c2-c3*power), 1/m1)
}

// hlgDecode applies the inverse HLG OETF, giving relative scene light. The
// OOTF that turns it into display light needs all three channels and runs in
// the tone-mapping stage.
func hlgDecode(value float64) float64 {
	const (
		a = 0.17883277
		b = 0.28466892
		c = 0.55991073
	)

	if value <= 0.5 {
		return value * value / 3
	}

	return (math.Exp((value-c)/a) + b) / 12
}

type matrix [3][3]float64

func (m matrix) apply(r, g, b float64) (float64, float64, float64) {
	return m[0][0]*r + m[0][1]*g + m[0][2]*b,
		m[1][0]*r + m[1][1]*g + m[1][2]*b,
		m[2][0]*r + m[2][1]*g + m[2][2]*b
}

func (m matrix) times(o matrix) matrix {
	var result matrix

	for row := range 3 {
		for column := range 3 {
			for k := range 3 {
				result[row][column] += m[row][k] * o[k][column]
			}
		}
	}

	return result
}

func (m matrix) inverse() matrix {
	determinant := m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	if determinant == 0 {
		return matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	}

	return matrix{
		{(m[1][1]*m[2][2] - m[1][2]*m[2][1]) / determinant, (m[0][2]*m[2][1] - m[0][1]*m[2][2]) / determinant, (m[0][1]*m[1][2] - m[0][2]*m[1][1]) / determinant},
		{(m[1][2]*m[2][0] - m[1][0]*m[2][2]) / determinant, (m[0][0]*m[2][2] - m[0][2]*m[2][0]) / determinant, (m[0][2]*m[1][0] - m[0][0]*m[1][2]) / determinant},
		{(m[1][0]*m[2][1] - m[1][1]*m[2][0]) / determinant, (m[0][1]*m[2][0] - m[0][0]*m[2][1]) / determinant, (m[0][0]*m[1][1] - m[0][1]*m[1][0]) / determinant},
	}
}

func xyz(point chromaticity) (float64, float64, float64) {
	return point.x / point.y, 1, (1 - point.x - point.y) / point.y
}

var (
	whiteD50 = [3]float64{0.9642, 1, 0.8249}
	bradford = matrix{{0.8951, 0.2664, -0.1614}, {-0.7502, 1.7135, 0.0367}, {0.0389, -0.0685, 1.0296}}
)

// primaryMatrix builds the linear RGB to XYZ matrix for a set of primaries,
// adapted from their white point to D50 with the Bradford transform.
func primaryMatrix(p primaries) matrix {
	var columns matrix

	for index, point := range []chromaticity{p.red, p.green, p.blue} {
		x, y, z := xyz(point)
		columns[0][index], columns[1][index], columns[2][index] = x, y, z
	}

	wx, wy, wz := xyz(p.white)
	sr, sg, sb := columns.inverse().apply(wx, wy, wz)

	for row := range 3 {
		columns[row][0] *= sr
		columns[row][1] *= sg
		columns[row][2] *= sb
	}

	return adaptation([3]float64{wx, wy, wz}).times(columns)
}

func adaptation(source [3]float64) matrix {
	sr, sg, sb := bradford.apply(source[0], source[1], source[2])
	dr, dg, db := bradford.apply(whiteD50[0], whiteD50[1], whiteD50[2])
	scale := matrix{{dr / sr, 0, 0}, {0, dg / sg, 0}, {0, 0, db / sb}}

	return bradford.inverse().times(scale).times(bradford)
}
