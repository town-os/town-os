package packages

import "strings"

// DependencySeparator is the marker used to namespace dependency package names
// under their parent. For example, parent "myapp" with dependency key "db"
// produces effective name "myapp--dep--db".
const DependencySeparator = "--dep--"

// SubpackagesDir is the reserved subvolume name used to encapsulate dependency
// packages on disk underneath their parent. An effective dependency name like
// "parent--dep--key" maps to the storage path "parent/subpackages/key" — the
// parent's install dir contains a `subpackages` subvolume that groups every
// dep keyed by its dep key, recursively.
const SubpackagesDir = "subpackages"

// DependenciesFile is the filename used to persist dependency records alongside
// the installed package.
const DependenciesFile = "dependencies.json"

// DependencyRecord describes a resolved dependency that was installed as part
// of a parent package's install flow.
type DependencyRecord struct {
	EffectiveName string `json:"effective_name"`
	Package       string `json:"package"`
	Repo          string `json:"repo"`
	Version       string `json:"version"`
}

// InputPackageDependency declares a dependency on another package in the
// repository system. It appears in the package YAML under the "dependencies"
// map.
type InputPackageDependency struct {
	Package   string            `yaml:"package" json:"package"`
	Repo      string            `yaml:"repo,omitempty" json:"repo,omitempty"`
	Version   string            `yaml:"version,omitempty" json:"version,omitempty"`
	Responses map[string]string `yaml:"responses,omitempty" json:"responses,omitempty"`
}

// DependencyName constructs the namespaced effective name for a dependency
// by joining the parent name and dependency key with the DependencySeparator.
func DependencyName(parentName, depKey string) string {
	return parentName + DependencySeparator + depKey
}

// IsDependency returns true if the package name contains the dependency
// separator, indicating it was installed as a dependency of another package.
func IsDependency(name string) bool {
	return strings.Contains(name, DependencySeparator)
}

// ParentName returns the immediate parent package name for a dependency.
// For "myapp--dep--db" it returns "myapp".
// For nested deps like "myapp--dep--db--dep--backup" it returns "myapp--dep--db".
// For non-dependency names it returns the name unchanged.
func ParentName(name string) string {
	idx := strings.LastIndex(name, DependencySeparator)
	if idx < 0 {
		return name
	}
	return name[:idx]
}

// StoragePath translates a flat effective name such as
// "parent--dep--key--dep--sub" into the nested on-disk form
// "parent/subpackages/key/subpackages/sub". Standalone package names (no
// DependencySeparator) pass through unchanged. This is the single source of
// truth for disk-layout nesting — every call site that previously joined the
// flat name under installed/<repo>/ routes through this helper.
func StoragePath(name string) string {
	segs := strings.Split(name, DependencySeparator)
	if len(segs) == 1 {
		return segs[0]
	}
	var b strings.Builder
	// Pre-compute an approximate cap: parent + (sep + subpackages + sep) * (n-1).
	b.Grow(len(name) + (len(SubpackagesDir)+2)*(len(segs)-1))
	b.WriteString(segs[0])
	for _, s := range segs[1:] {
		b.WriteByte('/')
		b.WriteString(SubpackagesDir)
		b.WriteByte('/')
		b.WriteString(s)
	}
	return b.String()
}

// ParseStoragePath reverses StoragePath: "parent/subpackages/key/subpackages/sub"
// → "parent--dep--key--dep--sub". Input paths without the SubpackagesDir
// marker are returned unchanged, so standalone names round-trip cleanly.
func ParseStoragePath(relPath string) string {
	return strings.ReplaceAll(relPath, "/"+SubpackagesDir+"/", DependencySeparator)
}

// PrettyName returns the human-facing display form for a dependency-aware
// name, using '/' as the segment separator so deps render as "parent/key"
// instead of "parent--dep--key". Standalone names pass through unchanged.
// This is the form surfaced on display_identifier API fields for UI / CLI
// consumers that want to reflect the actual packaging structure.
func PrettyName(name string) string {
	return strings.ReplaceAll(name, DependencySeparator, "/")
}
