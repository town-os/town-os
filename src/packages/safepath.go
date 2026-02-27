package packages

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafePath joins the parts under baseDir and validates that the resulting
// path does not escape baseDir via directory traversal (e.g. "..").
func SafePath(baseDir string, parts ...string) (string, error) {
	elems := make([]string, 0, 1+len(parts))
	elems = append(elems, baseDir)
	elems = append(elems, parts...)
	joined := filepath.Clean(filepath.Join(elems...))

	base := filepath.Clean(baseDir) + string(filepath.Separator)
	if joined != filepath.Clean(baseDir) && !strings.HasPrefix(joined, base) {
		return "", fmt.Errorf("path %q escapes base directory %q", joined, baseDir)
	}

	return joined, nil
}
