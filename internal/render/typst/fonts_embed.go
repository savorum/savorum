package typst

import _ "embed"

// Font bytes are embedded directly to avoid copying the complete font bundle
// onto the Go heap. Consumers must keep these shared bytes immutable.

//go:embed fonts/NotoEmoji.ttf
var fontNotoEmoji []byte

//go:embed fonts/NotoNaskhArabic-Regular.otf
var fontNotoNaskhArabicRegular []byte

//go:embed fonts/NotoNaskhArabic-SemiBold.otf
var fontNotoNaskhArabicSemibold []byte

//go:embed fonts/NotoSansCJKjp-Regular.otf
var fontNotoSansCJKJPRegular []byte

//go:embed fonts/NotoSansHebrew-Regular.otf
var fontNotoSansHebrewRegular []byte

//go:embed fonts/NotoSansHebrew-SemiBold.otf
var fontNotoSansHebrewSemibold []byte

//go:embed fonts/NotoSansSymbols2-Regular.ttf
var fontNotoSansSymbols2Regular []byte

//go:embed fonts/NotoSansThai-Regular.otf
var fontNotoSansThaiRegular []byte

//go:embed fonts/NotoSansThai-SemiBold.otf
var fontNotoSansThaiSemibold []byte

//go:embed fonts/SourceSans3-Regular.otf
var fontSourceSans3Regular []byte

//go:embed fonts/SourceSans3-Semibold.otf
var fontSourceSans3Semibold []byte

//go:embed fonts/SourceSerif4-It.otf
var fontSourceSerif4Italic []byte

//go:embed fonts/SourceSerif4-Regular.otf
var fontSourceSerif4Regular []byte

//go:embed fonts/SourceSerif4-Semibold.otf
var fontSourceSerif4Semibold []byte

var embeddedFonts = []struct {
	name     string
	contents []byte
}{
	{"NotoEmoji.ttf", fontNotoEmoji},
	{"NotoNaskhArabic-Regular.otf", fontNotoNaskhArabicRegular},
	{"NotoNaskhArabic-SemiBold.otf", fontNotoNaskhArabicSemibold},
	{"NotoSansCJKjp-Regular.otf", fontNotoSansCJKJPRegular},
	{"NotoSansHebrew-Regular.otf", fontNotoSansHebrewRegular},
	{"NotoSansHebrew-SemiBold.otf", fontNotoSansHebrewSemibold},
	{"NotoSansSymbols2-Regular.ttf", fontNotoSansSymbols2Regular},
	{"NotoSansThai-Regular.otf", fontNotoSansThaiRegular},
	{"NotoSansThai-SemiBold.otf", fontNotoSansThaiSemibold},
	{"SourceSans3-Regular.otf", fontSourceSans3Regular},
	{"SourceSans3-Semibold.otf", fontSourceSans3Semibold},
	{"SourceSerif4-It.otf", fontSourceSerif4Italic},
	{"SourceSerif4-Regular.otf", fontSourceSerif4Regular},
	{"SourceSerif4-Semibold.otf", fontSourceSerif4Semibold},
}
