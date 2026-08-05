package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GHAPP_TEST_RUN_MAIN") == "1" {
		os.Exit(run(os.Args))
	}
	os.Exit(m.Run())
}
