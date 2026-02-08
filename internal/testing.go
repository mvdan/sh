// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import (
	"os"
	"os/exec"
	"strings"
)

// TestMainSetup is used by the integration tests running shell scripts
// either via our interpreter or via real shells,
// to ensure a reasonably clean and consistent environment.
func TestMainSetup() {
	// Set the locale to computer-friendly English and UTF-8.
	// Some systems like macOS miss C.UTF8, so fall back to the US English locale.
	if out, _ := exec.Command("locale", "-a").Output(); strings.Contains(
		strings.ToLower(string(out)), "c.utf",
	) {
		os.Setenv("LANGUAGE", "C.UTF-8")
		os.Setenv("LC_ALL", "C.UTF-8")
	} else {
		os.Setenv("LANGUAGE", "en_US.UTF-8")
		os.Setenv("LC_ALL", "en_US.UTF-8")
	}
}
