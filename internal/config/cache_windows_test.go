//go:build windows

package config

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	value, existed := os.LookupEnv("LOCALAPPDATA")
	_ = os.Unsetenv("LOCALAPPDATA")
	code := m.Run()
	if existed {
		_ = os.Setenv("LOCALAPPDATA", value)
	}
	os.Exit(code)
}
