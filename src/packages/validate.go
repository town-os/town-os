package packages

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var (
	ErrInvalidImage          = errors.New("invalid container image")
	ErrInvalidImageType      = errors.New("invalid image type")
	ErrInvalidEnvironmentKey = errors.New("invalid environment key")
	ErrInvalidQuestionName   = errors.New("invalid question name")
	ErrInvalidMountpoint     = errors.New("invalid mountpoint")
	ErrInvalidVolumeName     = errors.New("invalid volume name")
	ErrInvalidArchiveSpec    = errors.New("invalid archive spec")
	ErrInvalidGitURL         = errors.New("invalid git URL")
	ErrInvalidTemplateName   = errors.New("invalid template name")
	ErrInvalidTemplateSpec   = errors.New("invalid template spec")
	ErrInvalidTemplatePath   = errors.New("invalid template path")
	ErrInvalidProtonSpec     = errors.New("invalid proton spec")
	ErrInvalidDependencyName = errors.New("invalid dependency name")
	ErrInvalidDependencySpec = errors.New("invalid dependency spec")
)

var (
	envKeyRegexp      = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	questionRegexp    = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
	volumeNameRegexp  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	imageRefRegexp    = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9._:/@-]*$`)
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
			image = "docker.io/library/" + image + ":latest"
		} else {
			image = "docker.io/library/" + image
		}
	case 2:
		// Two components: "user/app" or "user/app:v1"
		// But only if the first component doesn't contain a dot (which would
		// indicate a registry hostname like "ghcr.io").
		if !strings.Contains(parts[0], ".") && !strings.Contains(parts[0], ":") {
			if !hasTag {
				image = "docker.io/" + image + ":latest"
			} else {
				image = "docker.io/" + image
			}
		} else {
			// Registry/image with no namespace (e.g. "ghcr.io/app")
			if !hasTag {
				image += ":latest"
			}
		}
	default:
		// Full reference: leave unchanged except for tag
		if !hasTag {
			image += ":latest"
		}
	}

	return image
}

// ValidateImageURL checks that a container image reference is non-empty and
// contains only characters valid in OCI image references. Shell metacharacters
// (;, $, backticks, spaces, etc.) are rejected to prevent command injection.
// The @ character is allowed for template variables (e.g. "debian:@tag@") and
// digest references.
func ValidateImageURL(image string) error {
	if image == "" {
		return fmt.Errorf("%w: image must not be empty", ErrInvalidImage)
	}
	if !imageRefRegexp.MatchString(image) {
		return fmt.Errorf("%w: %q contains invalid characters", ErrInvalidImage, image)
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

// ValidateGitSource validates an InputPackageGitSource entry: the URL and
// Volume must be non-empty, and the Volume must reference an existing volume
// in the package definition (unless the volume name contains template chars).
func ValidateGitSource(gs InputPackageGitSource, volumes map[string]InputPackageVolume) error {
	if gs.URL == "" {
		return fmt.Errorf("%w: url must not be empty", ErrInvalidGitSource)
	}
	if gs.Volume == "" {
		return fmt.Errorf("%w: volume must not be empty", ErrInvalidGitSource)
	}
	if !strings.ContainsRune(gs.Volume, TemplateChar) {
		if _, ok := volumes[gs.Volume]; !ok {
			return fmt.Errorf("%w: volume %q not found in package volumes", ErrInvalidGitSource, gs.Volume)
		}
	}
	return nil
}

// ValidateArchiveSpec validates an InputPackageArchive entry: the image must
// be non-empty, the directory must be an absolute path, and the volume must
// reference an existing volume in the package definition.
func ValidateArchiveSpec(archive InputPackageArchive, volumes map[string]InputPackageVolume) error {
	if archive.Image == "" {
		return fmt.Errorf("%w: image must not be empty", ErrInvalidArchiveSpec)
	}
	if !strings.HasPrefix(archive.Directory, "/") {
		return fmt.Errorf("%w: directory %q must be an absolute path", ErrInvalidArchiveSpec, archive.Directory)
	}
	if _, ok := volumes[archive.Volume]; !ok {
		return fmt.Errorf("%w: volume %q not found in package volumes", ErrInvalidArchiveSpec, archive.Volume)
	}
	return nil
}

// ValidateVMConfig validates an InputPackageVM configuration. The image field
// must be non-empty and may be a URL (http/https) or a local filename.
func ValidateVMConfig(vm *InputPackageVM) error {
	if vm.Image == "" {
		return fmt.Errorf("%w: image must not be empty", ErrInvalidVMConfig)
	}
	// Allow template variables in the image field.
	if strings.ContainsRune(vm.Image, TemplateChar) {
		return nil
	}
	// Validate URL format if it looks like a URL.
	if strings.HasPrefix(vm.Image, "http://") || strings.HasPrefix(vm.Image, "https://") {
		if _, err := url.Parse(vm.Image); err != nil {
			return fmt.Errorf("%w: invalid image URL: %w", ErrInvalidVMConfig, err)
		}
	}
	if vm.CPUs < 0 {
		return fmt.Errorf("%w: cpus must be non-negative", ErrInvalidVMConfig)
	}
	return nil
}

// ValidateImageType checks that the image type is a recognized value.
// Empty defaults to "oci". Currently only "oci" is accepted.
func ValidateImageType(t string) error {
	switch t {
	case "", ImageTypeOCI:
		return nil
	default:
		return fmt.Errorf("%w: %q (must be %q)", ErrInvalidImageType, t, ImageTypeOCI)
	}
}

// ValidateProtonSpec validates an InputPackageProton entry: app_image must be
// non-empty, app_directory must be an absolute path, volume must reference an
// existing volume, and exe must be non-empty.
func ValidateProtonSpec(proton InputPackageProton, volumes map[string]InputPackageVolume) error {
	if proton.AppImage == "" {
		return fmt.Errorf("%w: app_image must not be empty", ErrInvalidProtonSpec)
	}
	if !strings.HasPrefix(proton.AppDirectory, "/") {
		return fmt.Errorf("%w: app_directory %q must be an absolute path", ErrInvalidProtonSpec, proton.AppDirectory)
	}
	if _, ok := volumes[proton.Volume]; !ok {
		return fmt.Errorf("%w: volume %q not found in package volumes", ErrInvalidProtonSpec, proton.Volume)
	}
	if proton.Exe == "" {
		return fmt.Errorf("%w: exe must not be empty", ErrInvalidProtonSpec)
	}
	return nil
}

// ValidateGitURL validates a git repository URL. Empty strings are accepted
// (no git seed). Non-empty values must have a valid scheme and host.
func ValidateGitURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidGitURL, err)
	}
	if u.Scheme == "" || (u.Host == "" && u.Scheme != "file") {
		return fmt.Errorf("%w: %q (must include scheme and host)", ErrInvalidGitURL, rawURL)
	}
	return nil
}

// ValidateTemplateName checks that a template name matches the volume naming
// convention: starts with an alphanumeric character, followed by alphanumeric
// characters, dots, dashes, or underscores.
func ValidateTemplateName(name string) error {
	if !volumeNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidTemplateName, name)
	}
	return nil
}

// ValidateTemplateSpec validates an InputPackageTemplate entry: the volume
// must be non-empty, the path must be non-empty, and the content must be
// non-empty. If the volume does not contain template characters, it must
// reference an existing volume in the package definition.
func ValidateTemplateSpec(tmpl InputPackageTemplate, volumes map[string]InputPackageVolume) error {
	if tmpl.Volume == "" {
		return fmt.Errorf("%w: volume must not be empty", ErrInvalidTemplateSpec)
	}
	if tmpl.Path == "" {
		return fmt.Errorf("%w: path must not be empty", ErrInvalidTemplateSpec)
	}
	if tmpl.Content == "" {
		return fmt.Errorf("%w: content must not be empty", ErrInvalidTemplateSpec)
	}
	if !strings.ContainsRune(tmpl.Volume, TemplateChar) {
		if _, ok := volumes[tmpl.Volume]; !ok {
			return fmt.Errorf("%w: volume %q not found in package volumes", ErrInvalidTemplateSpec, tmpl.Volume)
		}
	}
	return nil
}

// ValidateTemplatePath checks that a template file path is a relative path
// without directory traversal sequences.
func ValidateTemplatePath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: path must not be empty", ErrInvalidTemplatePath)
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: %q (must be a relative path)", ErrInvalidTemplatePath, path)
	}
	if slices.Contains(strings.Split(path, "/"), "..") {
		return fmt.Errorf("%w: %q (must not contain directory traversal)", ErrInvalidTemplatePath, path)
	}
	return nil
}

// ValidateDependencyName checks that a dependency key follows the volume
// naming convention (alphanumeric with dots, dashes, and underscores) and
// is not the reserved SubpackagesDir name used to encapsulate dep storage
// on disk. Using "subpackages" as a dep key would collide with the
// nesting container directory and corrupt the installed-package walker.
func ValidateDependencyName(name string) error {
	if !volumeNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidDependencyName, name)
	}
	if name == SubpackagesDir {
		return fmt.Errorf("%w: %q is a reserved subpackages directory name", ErrInvalidDependencyName, name)
	}
	return nil
}

// ValidateDependencySpec validates that a dependency declaration has a
// non-empty package field.
func ValidateDependencySpec(dep InputPackageDependency) error {
	if dep.Package == "" {
		return fmt.Errorf("%w: package must not be empty", ErrInvalidDependencySpec)
	}
	return nil
}

// ValidatePackageName checks that a package name does not contain the
// dependency separator (reserved for dependency namespacing) and is not
// the reserved SubpackagesDir encapsulator — a package literally called
// "subpackages" would collide with the on-disk nesting container directory
// produced by StoragePath.
func ValidatePackageName(name string) error {
	if strings.Contains(name, DependencySeparator) {
		return fmt.Errorf("%w: %q contains reserved separator %q", ErrInvalidDependencyName, name, DependencySeparator)
	}
	if name == SubpackagesDir {
		return fmt.Errorf("%w: %q is a reserved subpackages directory name", ErrInvalidDependencyName, name)
	}
	return nil
}
