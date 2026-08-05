package wrapper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func FindRealBinary(name, configured string) (string, error) {
	if configured != "" {
		return validateConfiguredBinary(name, configured)
	}

	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate ghapp executable: %w", err)
	}
	selfInfo, _ := os.Stat(self)

	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if directory == "" {
			directory = "."
		}
		for _, candidateName := range executableNames(name) {
			candidate := filepath.Join(directory, candidateName)
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() || !isExecutable(candidate, info) {
				continue
			}
			if selfInfo != nil && os.SameFile(selfInfo, info) {
				continue
			}
			absolute, err := filepath.Abs(candidate)
			if err == nil {
				candidate = absolute
			}
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find underlying %s executable; set GHAPP_REAL_%s", name, strings.ToUpper(name))
}

func validateConfiguredBinary(name, configured string) (string, error) {
	path := configured
	self, selfErr := os.Executable()
	var selfInfo os.FileInfo
	if selfErr == nil {
		selfInfo, _ = os.Stat(self)
	}
	bareName := !strings.ContainsRune(configured, filepath.Separator) && !(runtime.GOOS == "windows" && strings.Contains(configured, "/"))
	if bareName {
		found := false
		for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
			if directory == "" {
				directory = "."
			}
			for _, candidateName := range executableNames(configured) {
				candidate := filepath.Join(directory, candidateName)
				info, err := os.Stat(candidate)
				if err != nil || info.IsDir() || !isExecutable(candidate, info) {
					continue
				}
				if selfInfo != nil && os.SameFile(selfInfo, info) {
					continue
				}
				path = candidate
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			return "", fmt.Errorf("configured %s executable %q was not found on PATH outside ghapp", name, configured)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("configured %s executable %q: %w", name, path, err)
	}
	if info.IsDir() || !isExecutable(path, info) {
		return "", fmt.Errorf("configured %s path %q is not executable", name, path)
	}
	if selfInfo != nil && os.SameFile(selfInfo, info) {
		return "", errors.New("configured underlying executable points back to ghapp")
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	return path, nil
}

func executableNames(name string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(name) != "" {
		return []string{name}
	}
	extensions := filepath.SplitList(strings.ReplaceAll(os.Getenv("PATHEXT"), ";", string(os.PathListSeparator)))
	if len(extensions) == 0 {
		extensions = []string{".exe", ".cmd", ".bat", ".com"}
	}
	result := []string{name}
	for _, extension := range extensions {
		if extension != "" {
			result = append(result, name+strings.ToLower(extension), name+strings.ToUpper(extension))
		}
	}
	return result
}

func isExecutable(path string, info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		extension := strings.ToLower(filepath.Ext(path))
		return extension == ".exe" || extension == ".cmd" || extension == ".bat" || extension == ".com"
	}
	return info.Mode().Perm()&0o111 != 0
}
