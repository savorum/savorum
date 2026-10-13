package importer

import (
	"os/exec"
	"runtime"
)

// AvailableFormats reports declarations supported by this runtime. Registered
// adapters remain available for explicit imports and descriptive failure reports.
func (r Registry) AvailableFormats() []Format {
	available := []Adapter{}

	for _, adapter := range r.adapters {
		if runtimeAdapter, ok := adapter.(interface{ AvailableFormat() (Format, bool) }); ok {
			format, enabled := runtimeAdapter.AvailableFormat()
			if enabled {
				available = append(available, availableAdapter{Adapter: adapter, format: format})
			}
		} else {
			available = append(available, adapter)
		}
	}

	return NewRegistry(available...).Formats()
}

type availableAdapter struct {
	Adapter
	format Format
}

func (a availableAdapter) Format() Format { return a.format }

// AvailableFormat distinguishes local native tools from supplied OCR providers.
func (a documentAdapter) AvailableFormat() (Format, bool) {
	if a.OCR != nil {
		return a.Format(), true
	}

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return Format{}, false
	}

	return a.formatForTools(func(name string) bool { _, err := exec.LookPath(name); return err == nil })
}

func (a documentAdapter) formatForTools(present func(string) bool) (Format, bool) {
	if !present("/bin/sh") {
		return Format{}, false
	}

	format := a.Format()
	format.Extensions = nil

	format.MediaTypes = nil
	if present("pdfinfo") && present("pdftotext") {
		format.Extensions = append(format.Extensions, ".pdf")
		format.MediaTypes = append(format.MediaTypes, "application/pdf")
	}

	if present("tesseract") {
		format.Extensions = append(format.Extensions, ".png", ".jpg", ".jpeg")
		format.MediaTypes = append(format.MediaTypes, "image/png", "image/jpeg")
	}

	if len(format.Extensions) == 1 {
		format.Name = "PDF"
	} else if len(format.Extensions) == 3 {
		format.Name = "Image"
	}

	return format, len(format.Extensions) > 0
}
