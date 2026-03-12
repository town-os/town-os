package packages

import "strings"

// DependencySeparator is the marker used to namespace dependency package names
// under their parent. For example, parent "myapp" with dependency key "db"
// produces effective name "myapp--dep--db".
const DependencySeparator = "--dep--"

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
