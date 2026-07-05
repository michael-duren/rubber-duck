package main

import (
	"fmt"
	"runtime/debug"
)

// version is stamped by the release build via
// -ldflags "-X main.version=...". Builds that skip the stamp (go install
// module@version, local go build) fall back to the module build info.
var version = "dev"

func versionCmd(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: duck version")
	}
	v := version
	if v == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			v = bi.Main.Version
		}
	}
	fmt.Println("duck", v)
	return nil
}
