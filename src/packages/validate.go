package packages

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrInvalidImage          = fmt.Errorf("invalid container image")
	ErrInvalidEnvironmentKey = fmt.Errorf("invalid environment key")
	ErrInvalidQuestionName   = fmt.Errorf("invalid question name")
	ErrInvalidMountpoint     = fmt.Errorf("invalid mountpoint")
	ErrInvalidVolumeName     = fmt.Errorf("invalid volume name")
)

var (
	envKeyRegexp      = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	questionRegexp    = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
	volumeNameRegexp  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
)

// NormalizeImageURL normalizes a container image reference:
//   - single name (e.g. "nginx") → "docker.io/library/nginx:latest"
//   - two components (e.g. "user/app") → "docker.io/user/app:latest"
//   - full reference (e.g. "ghcr.io/user/app:v1") → unchanged
//   - appends ":latest" when no tag is present
func NormalizeImageURL(image string) string {
	if image == "" {
		return image
	}

	// Split off the tag portion. A tag follows the last colon that is NOT part
	// of a port or registry (i.e. the part after the last slash).
	hasTag := false
	lastSlash := strings.LastIndex(image, "/")
	afterSlash := image
	if lastSlash >= 0 {
		afterSlash = image[lastSlash+1:]
	}
	if strings.Contains(afterSlash, ":") {
		hasTag = true
	}

	// Count slashes to determine component count.
	parts := strings.Split(image, "/")
	switch len(parts) {
	case 1:
		// Single name: "nginx" or "nginx:1.0"
		if !hasTag {
			image = fmt.Sprintf("docker.io/library/%s:latest", image)
		} else {
			image = fmt.Sprintf("docker.io/library/%s", image)
		}
	case 2:
		// Two components: "user/app" or "user/app:v1"
		// But only if the first component doesn't contain a dot (which would
		// indicate a registry hostname like "ghcr.io").
		if !strings.Contains(parts[0], ".") && !strings.Contains(parts[0], ":") {
			if !hasTag {
				image = fmt.Sprintf("docker.io/%s:latest", image)
			} else {
				image = fmt.Sprintf("docker.io/%s", image)
			}
		} else {
			// Registry/image with no namespace (e.g. "ghcr.io/app")
			if !hasTag {
				image = fmt.Sprintf("%s:latest", image)
			}
		}
	default:
		// Full reference: leave unchanged except for tag
		if !hasTag {
			image = fmt.Sprintf("%s:latest", image)
		}
	}

	return image
}

// ValidateImageURL checks that a container image reference is non-empty.
func ValidateImageURL(image string) error {
	if image == "" {
		return fmt.Errorf("%w: image must not be empty", ErrInvalidImage)
	}
	return nil
}

// ValidateEnvironmentKey checks that an environment variable key matches
// the POSIX convention: starts with a letter or underscore, followed by
// letters, digits, or underscores.
func ValidateEnvironmentKey(key string) error {
	if !envKeyRegexp.MatchString(key) {
		return fmt.Errorf("%w: %q", ErrInvalidEnvironmentKey, key)
	}
	return nil
}

// ValidateQuestionName checks that a question/template variable name
// contains only alphanumeric characters.
func ValidateQuestionName(name string) error {
	if !questionRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q (must be alphanumeric)", ErrInvalidQuestionName, name)
	}
	return nil
}

// ValidateMountpoint checks that a container mountpoint is an absolute path.
func ValidateMountpoint(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: %q (must start with /)", ErrInvalidMountpoint, path)
	}
	return nil
}

// ValidateVolumeName checks that a volume name matches the storage naming
// convention: starts with an alphanumeric character, followed by alphanumeric
// characters, dots, dashes, or underscores.
func ValidateVolumeName(name string) error {
	if !volumeNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidVolumeName, name)
	}
	return nil
}
