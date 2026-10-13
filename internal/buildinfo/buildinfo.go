// Package buildinfo holds release identity injected by official builds.
package buildinfo

import "runtime/debug"

// Version and Revision are set with -ldflags by release builds. Development
// builds retain useful Go module/VCS metadata when the toolchain provides it.
var (
	Version  = "development"
	Revision = ""
)

// CurrentVersion returns the version embedded in this binary.
func CurrentVersion() string {
	if Version != "" && Version != "development" {
		return Version
	}

	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	return "development"
}
