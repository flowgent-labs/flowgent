package console

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectFormat returns the format ("json" or "yaml") based on file extension
// or an explicit --format flag in args.
func DetectFormat(filePath string, args []string) (string, error) {
	for i, a := range args {
		if a == "--format" && i+1 < len(args) {
			f := strings.ToLower(args[i+1])
			if f == "yml" {
				f = "yaml"
			}
			if f != "json" && f != "yaml" {
				return "", fmt.Errorf("unknown format: %s (use json or yaml)", args[i+1])
			}
			return f, nil
		}
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".json":
		return "json", nil
	case ".yaml", ".yml":
		return "yaml", nil
	default:
		return "", fmt.Errorf("cannot determine format from extension %q, use --format json|yaml", ext)
	}
}

// CollectDirFiles recursively collects .yaml/.yml/.json files from a directory.
func CollectDirFiles(dirPath string, files *[]string, seen *map[string]bool) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			CollectDirFiles(filepath.Join(dirPath, e.Name()), files, seen)
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		fp := filepath.Join(dirPath, e.Name())
		if !(*seen)[fp] {
			(*seen)[fp] = true
			*files = append(*files, fp)
		}
	}
}
