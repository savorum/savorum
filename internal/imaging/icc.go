package imaging

import (
	"encoding/binary"
	"math"
)

// iccSpace interprets an embedded ICC profile as a color space. Only the RGB
// matrix-and-transfer form resolves: the profile's colorant columns already
// sit in the D50 connection space, so they become the toXYZ matrix as stored.
// Anything else — Lab connection space, non-matrix profiles, damaged tables —
// reports false and the caller treats the image as sRGB.
func iccSpace(icc []byte) (ColorSpace, bool) {
	if len(icc) < 132 || string(icc[36:40]) != "acsp" {
		return ColorSpace{}, false
	}

	if string(icc[16:20]) != "RGB " || string(icc[20:24]) != "XYZ " {
		return ColorSpace{}, false
	}

	tags := iccTags(icc)

	var space ColorSpace

	for index, signature := range []string{"rXYZ", "gXYZ", "bXYZ"} {
		column, ok := iccXYZ(tags[signature])
		if !ok {
			return ColorSpace{}, false
		}

		space.toXYZ[0][index], space.toXYZ[1][index], space.toXYZ[2][index] = column[0], column[1], column[2]
	}

	for index, signature := range []string{"rTRC", "gTRC", "bTRC"} {
		curve, ok := iccCurve(tags[signature])
		if !ok {
			return ColorSpace{}, false
		}

		space.transfer[index] = curve
	}

	space.srgb = space.matchesSRGB()

	return space, true
}

func iccTags(icc []byte) map[string][]byte {
	tags := map[string][]byte{}
	count := int(binary.BigEndian.Uint32(icc[128:132]))

	for index := range count {
		at := 132 + index*12
		if at+12 > len(icc) {
			break
		}

		offset := int(binary.BigEndian.Uint32(icc[at+4:]))

		size := int(binary.BigEndian.Uint32(icc[at+8:]))
		if offset < 0 || size < 0 || offset+size > len(icc) {
			continue
		}

		if _, seen := tags[string(icc[at:at+4])]; !seen {
			tags[string(icc[at:at+4])] = icc[offset : offset+size]
		}
	}

	return tags
}

func s15fixed(value []byte) float64 {
	return float64(int32(binary.BigEndian.Uint32(value))) / 65536 //nolint:gosec // the field is a signed fixed-point number
}

func iccXYZ(element []byte) ([3]float64, bool) {
	if len(element) < 20 || string(element[:4]) != "XYZ " {
		return [3]float64{}, false
	}

	return [3]float64{s15fixed(element[8:]), s15fixed(element[12:]), s15fixed(element[16:])}, true
}

// iccCurve reads a tone reproduction curve: a 'curv' gamma or table, or a
// 'para' parametric function of type 0 through 4.
func iccCurve(element []byte) (func(float64) float64, bool) {
	if len(element) < 12 {
		return nil, false
	}

	switch string(element[:4]) {
	case "curv":
		count := int(binary.BigEndian.Uint32(element[8:]))
		switch {
		case count == 0:
			return func(value float64) float64 { return value }, true
		case count == 1 && len(element) >= 14:
			gamma := float64(binary.BigEndian.Uint16(element[12:])) / 256
			return func(value float64) float64 { return math.Pow(max(value, 0), gamma) }, true
		case len(element) >= 12+2*count:
			table := make([]float64, count)
			for index := range table {
				table[index] = float64(binary.BigEndian.Uint16(element[12+2*index:])) / 65535
			}

			return func(value float64) float64 {
				position := min(max(value, 0), 1) * float64(count-1)

				low := int(position)
				if low >= count-1 {
					return table[count-1]
				}

				fraction := position - float64(low)

				return table[low] + fraction*(table[low+1]-table[low])
			}, true
		}
	case "para":
		if len(element) < 14 {
			return nil, false
		}

		return iccParametric(element)
	}

	return nil, false
}

func iccParametric(element []byte) (func(float64) float64, bool) {
	kind := int(binary.BigEndian.Uint16(element[8:]))
	if kind > 4 {
		return nil, false
	}

	params := element[12:]
	if len(params) < 4*iccParamCount[kind] {
		return nil, false
	}

	p := func(index int) float64 { return s15fixed(params[4*index:]) }

	switch kind {
	case 0:
		return func(value float64) float64 { return math.Pow(max(value, 0), p(0)) }, true
	case 1, 2:
		return func(value float64) float64 {
			if linear := p(1)*value + p(2); linear >= 0 {
				return math.Pow(linear, p(0)) + iccParametricC(kind, p)
			}

			return iccParametricC(kind, p)
		}, true
	case 3:
		return func(value float64) float64 {
			if value >= p(4) {
				return math.Pow(p(1)*value+p(2), p(0))
			}

			return p(3) * value
		}, true
	case 4:
		return func(value float64) float64 {
			if value >= p(4) {
				return math.Pow(p(1)*value+p(2), p(0)) + p(5)
			}

			return p(3)*value + p(6)
		}, true
	}

	return nil, false
}

// iccParamCount is the s15Fixed16 parameter count of each parametric curve
// type the reader handles.
var iccParamCount = [5]int{1, 3, 4, 5, 7}

func iccParametricC(kind int, p func(int) float64) float64 {
	if kind == 2 {
		return p(3)
	}

	return 0
}

// matchesSRGB reports whether a resolved profile is indistinguishable from
// sRGB, so its image can skip conversion without changing any pixel.
func (s ColorSpace) matchesSRGB() bool {
	for row := range 3 {
		for column := range 3 {
			if math.Abs(s.toXYZ[row][column]-srgbToXYZ[row][column]) > 1e-3 {
				return false
			}
		}
	}

	for _, curve := range s.transfer {
		for _, sample := range []float64{0.02, 0.05, 0.18, 0.5, 0.8, 1} {
			if math.Abs(curve(sample)-srgbDecode(sample)) > 5e-3 {
				return false
			}
		}
	}

	return true
}
