package main

import (
	"strings"
	"testing"
)

func TestVersionStringStartsWithTheVersion(t *testing.T) {
	old := version
	defer func() { version = old }()

	version = "v1.2.3"
	if got := versionString(); !strings.HasPrefix(got, "v1.2.3") {
		t.Errorf("versionString = %q, want it to start with v1.2.3", got)
	}
}
