package wrapper

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func Run(path string, args, env []string) int {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		_, _ = os.Stderr.WriteString("viagh: " + err.Error() + "\n")
		return 1
	}
	stopForwarding := forwardSignals(cmd.Process)
	err := cmd.Wait()
	stopForwarding()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return processExitCode(exitErr)
		}
		_, _ = os.Stderr.WriteString("viagh: " + err.Error() + "\n")
		return 1
	}
	return 0
}

func Capture(path string, args, env []string) (stdout, stderr []byte, exitCode int, err error) {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	cmd.Env = env
	if startErr := cmd.Start(); startErr != nil {
		return nil, nil, 1, fmt.Errorf("run %s: %w", path, startErr)
	}
	stopForwarding := forwardSignals(cmd.Process)
	runErr := cmd.Wait()
	stopForwarding()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), processExitCode(exitErr), nil
		}
		return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), 1, fmt.Errorf("run %s: %w", path, runErr)
	}
	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), 0, nil
}
