package main

import (
	"fmt"
	"os"
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
	os.Exit(m.Run())
}
