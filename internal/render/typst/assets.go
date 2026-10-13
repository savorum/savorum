// Package typst typesets Savorum's printed documents behind the
// print.DocumentRenderer boundary. The compiler is an embedded WebAssembly
// module run in-process, with embedded templates and fonts, so printing needs
// no external program or network.
package typst

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"golang.org/x/image/font/sfnt"
)

//go:embed templates/*.typ
var templateFiles embed.FS

// EngineVersion names the typesetting engine the embedded compiler carries.
// It is part of the renderer version, so an engine upgrade cannot be served
// from a cache filled by the previous one.
const EngineVersion = "typst-0.14.2"

// mainTemplate is the only entry point. Every other template is a module it
// imports; none of them is reachable from a recipe.
const mainTemplate = "document.typ"

type bundle struct {
	// sources are the template modules, addressed by the names the main
	// template imports them under. This is the complete set of files a
	// document may reach: there is no filesystem and no package resolution.
	sources map[string]string
	fonts   [][]byte
	faces   []fontFace
	// version fingerprints the templates and fonts together, because either
	// changing changes the bytes a specification produces.
	version string
}

func loadBundle() (bundle, error) {
	loaded := bundle{sources: map[string]string{}}
	digest := sha256.New()
	digest.Write([]byte(EngineVersion))
	digest.Write([]byte("document-font-selection-v1"))

	names, err := sortedNames(templateFiles, "templates")
	if err != nil {
		return bundle{}, err
	}

	for _, name := range names {
		contents, err := templateFiles.ReadFile("templates/" + name)
		if err != nil {
			return bundle{}, fmt.Errorf("read template %s: %w", name, err)
		}

		loaded.sources[name] = string(contents)
		digest.Write([]byte(name))
		digest.Write(contents)
	}

	if _, found := loaded.sources[mainTemplate]; !found {
		return bundle{}, fmt.Errorf("the %s template is missing", mainTemplate)
	}

	for _, embedded := range embeddedFonts {
		name, contents := embedded.name, embedded.contents

		loaded.fonts = append(loaded.fonts, contents)

		face, err := sfnt.Parse(contents)
		if err != nil {
			return bundle{}, fmt.Errorf("read font coverage %s: %w", name, err)
		}

		family, _, _ := strings.Cut(strings.TrimSuffix(strings.TrimSuffix(name, ".otf"), ".ttf"), "-")
		loaded.faces = append(loaded.faces, fontFace{family: family, font: face})

		digest.Write([]byte(name))
		digest.Write(contents)
	}

	if len(loaded.fonts) == 0 {
		return bundle{}, fmt.Errorf("no print fonts are embedded")
	}

	loaded.version = EngineVersion + "+" + hex.EncodeToString(digest.Sum(nil))[:16]

	return loaded, nil
}

func sortedNames(files embed.FS, directory string) ([]string, error) {
	entries, err := fs.ReadDir(files, directory)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", directory, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	sort.Strings(names)

	return names, nil
}
