package imaging

import (
	"encoding/binary"
	"math"
	"testing"
)

func fixed(v float64) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(math.Round(v*65536)))

	return out
}

// srgbCurvePara encodes the sRGB transfer as a parametric type-3 curve, the
// form the canonical sRGB profile carries.
func srgbCurvePara() []byte {
	out := []byte{'p', 'a', 'r', 'a', 0, 0, 0, 0, 0, 3, 0, 0}
	for _, p := range []float64{2.4, 1 / 1.055, 0.055 / 1.055, 1 / 12.92, 0.04045} {
		out = append(out, fixed(p)...)
	}

	return out
}

func xyzElement(x, y, z float64) []byte {
	out := []byte{'X', 'Y', 'Z', ' ', 0, 0, 0, 0}
	for _, v := range []float64{x, y, z} {
		out = append(out, fixed(v)...)
	}

	return out
}

func gammaElement(gamma float64) []byte {
	out := []byte{'c', 'u', 'r', 'v', 0, 0, 0, 0, 0, 0, 0, 1}
	value := make([]byte, 2)
	binary.BigEndian.PutUint16(value, uint16(math.Round(gamma*256)))

	return append(out, value...)
}

// iccProfile assembles a minimal RGB matrix profile. Curves may be nil for a
// missing TRC.
func iccProfile(pcs string, colorants, curves [3][]byte) []byte {
	var (
		tags     []string
		elements [][]byte
	)

	for index, element := range colorants {
		if element != nil {
			tags = append(tags, []string{"rXYZ", "gXYZ", "bXYZ"}[index])
			elements = append(elements, element)
		}
	}

	for index, element := range curves {
		if element != nil {
			tags = append(tags, []string{"rTRC", "gTRC", "bTRC"}[index])
			elements = append(elements, element)
		}
	}

	header := 132 + 12*len(tags)

	total := header
	for _, element := range elements {
		total += len(element)
	}

	profile := make([]byte, total)
	copy(profile[12:], "mntr")
	copy(profile[16:], "RGB ")
	copy(profile[20:], pcs)
	copy(profile[36:], "acsp")
	binary.BigEndian.PutUint32(profile[128:], uint32(len(tags))) //nolint:gosec // a fixture never has many tags
	binary.BigEndian.PutUint32(profile[0:], uint32(total))       //nolint:gosec // a fixture is small

	offset := header

	for index, signature := range tags {
		at := 132 + index*12
		copy(profile[at:], signature)
		binary.BigEndian.PutUint32(profile[at+4:], uint32(offset))               //nolint:gosec // fixture offsets are small
		binary.BigEndian.PutUint32(profile[at+8:], uint32(len(elements[index]))) //nolint:gosec // fixture elements are small
		copy(profile[offset:], elements[index])
		offset += len(elements[index])
	}

	return profile
}

func srgbColorants() [3][]byte {
	return [3][]byte{
		xyzElement(srgbToXYZ[0][0], srgbToXYZ[1][0], srgbToXYZ[2][0]),
		xyzElement(srgbToXYZ[0][1], srgbToXYZ[1][1], srgbToXYZ[2][1]),
		xyzElement(srgbToXYZ[0][2], srgbToXYZ[1][2], srgbToXYZ[2][2]),
	}
}

func TestICCSpaceResolvesAnSRGBProfileToSRGB(t *testing.T) {
	curve := srgbCurvePara()
	profile := iccProfile("XYZ ", srgbColorants(), [3][]byte{curve, curve, curve})

	space, ok := iccSpace(profile)
	if !ok {
		t.Fatal("sRGB profile did not resolve")
	}

	if !space.srgb {
		t.Fatal("sRGB profile was not recognized as sRGB")
	}
}

func TestICCSpaceResolvesAWideGamutProfile(t *testing.T) {
	wide := primaryMatrix(cicpPrimaries[9])
	colorants := [3][]byte{
		xyzElement(wide[0][0], wide[1][0], wide[2][0]),
		xyzElement(wide[0][1], wide[1][1], wide[2][1]),
		xyzElement(wide[0][2], wide[1][2], wide[2][2]),
	}
	curve := gammaElement(2.2)

	space, ok := iccSpace(iccProfile("XYZ ", colorants, [3][]byte{curve, curve, curve}))
	if !ok {
		t.Fatal("wide-gamut profile did not resolve")
	}

	if space.srgb {
		t.Fatal("wide-gamut profile was mistaken for sRGB")
	}

	if space.HDR() {
		t.Fatal("gamma profile was treated as HDR")
	}
}

func TestICCSpaceReadsACurveTable(t *testing.T) {
	table := []byte{'c', 'u', 'r', 'v', 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0x80, 0, 0xff, 0xff}

	curve, ok := iccCurve(table)
	if !ok {
		t.Fatal("curve table did not parse")
	}

	if got := curve(0.5); math.Abs(got-0.5) > 0.02 {
		t.Fatalf("curve(0.5) = %v, want ~0.5", got)
	}
}

func TestICCSpaceRejectsWhatItCannotInterpret(t *testing.T) {
	for name, profile := range map[string][]byte{
		"short":         make([]byte, 60),
		"not a profile": make([]byte, 200),
		"lab pcs":       iccProfile("Lab ", srgbColorants(), [3][]byte{srgbCurvePara(), srgbCurvePara(), srgbCurvePara()}),
		"missing trc":   iccProfile("XYZ ", srgbColorants(), [3][]byte{nil, srgbCurvePara(), srgbCurvePara()}),
	} {
		if _, ok := iccSpace(profile); ok {
			t.Fatalf("%s profile resolved", name)
		}
	}
}
