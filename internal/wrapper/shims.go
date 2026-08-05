package wrapper

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func CreateShims(executable string) (string, func(), error) {
	directory, err := os.MkdirTemp("", "ghapp-shims-")
	if err != nil {
		return "", nil, fmt.Errorf("create shim directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	for _, name := range []string{"gh", "git"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		target := filepath.Join(directory, name)
		if err := os.Symlink(executable, target); err != nil {
			if copyErr := copyExecutable(executable, target); copyErr != nil {
				cleanup()
				return "", nil, fmt.Errorf("create %s shim: %w", name, copyErr)
			}
		}
	}
	return directory, cleanup, nil
}

func PrependPath(env []string, directory string) []string {
	path, _ := LookupEnv(env, "PATH")
	if path == "" {
		return SetEnv(env, "PATH", directory)
	}
	return SetEnv(env, "PATH", directory+string(os.PathListSeparator)+path)
}

func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
