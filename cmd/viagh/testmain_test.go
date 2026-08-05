package main

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("VIAGH_TEST_FAKE_GH") == "1" {
		fmt.Fprintln(os.Stderr, "underlying gh was unexpectedly invoked")
		os.Exit(99)
	}
	if os.Getenv("VIAGH_TEST_RUN_MAIN") == "1" {
		os.Exit(run(os.Args))
	}
	dir, err := os.MkdirTemp("", "viagh-test-config")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	if runtime.GOOS == "windows" {
		_ = os.Setenv("AppData", dir)
	} else {
		_ = os.Setenv("XDG_CONFIG_HOME", dir)
	}
	os.Exit(m.Run())
}
