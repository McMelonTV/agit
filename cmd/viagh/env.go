package main

import (
	"errors"
	"fmt"

	"github.com/McMelonTV/viagh/internal/config"
)

func runEnv(cfg config.Config, args []string) int {
	if len(args) != 1 {
		return fail(errors.New("env requires exactly one variable name"))
	}
	key := args[0]
	value, source, ok := config.LookupEnvSource(key)
	if !ok {
		return fail(fmt.Errorf("%s is not set", key))
	}
	fmt.Printf("%s=%s (%s)\n", key, value, source)
	return 0
}
