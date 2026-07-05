package main

import "fmt"

// version is stamped by the release build via
// -ldflags "-X main.version=...". Source builds report "dev".
var version = "dev"

func versionCmd(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: duck version")
	}
	fmt.Println("duck", version)
	return nil
}
