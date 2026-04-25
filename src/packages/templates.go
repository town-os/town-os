package packages

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// TemplatePackageInfo exposes package metadata to Go text/template rendering.
type TemplatePackageInfo struct {
	Name        string // Package name
	Version     string // Package version
	Repo        string // Repository name
	Image       string // Container image reference (compiled)
	Description string // Human-readable package description
}

// TemplateSystemInfo provides a facility for exposing system-level information
// to Go text/template rendering. Fields are populated by the caller; this
// struct acts as an extensible container for future additions such as
// hostnames, IP addresses, and other system properties.
type TemplateSystemInfo struct {
	Hostname   string // System hostname
	ExternalIP string // External IP address (if known)
	InternalIP string // Internal/LAN IP address (if known)
}

// TemplateDep exposes a single dependency's runtime coordinates to Go
// text/template rendering. Host is the podman container name (resolvable
// via podman DNS on the parent's shared network). Ports is keyed by both
// the numeric container port (e.g. "5432") and any semantic port name
// declared on the dep's network entry (e.g. "sql"), lowercased; the value
// is the container-side port value — identical under current single-network
// wiring, but the map permits future remapping without API churn. Volumes
// maps each shared (`shareable: true`) volume name to its in-dep mountpoint;
// non-shareable volumes are omitted so file templates cannot reach data the
// dep author did not opt in to expose.
type TemplateDep struct {
	Host    string
	Ports   map[string]string
	Volumes map[string]string
}

// TemplateData is the top-level data object passed to Go text/template
// execution for package file templates. It provides access to:
//   - Responses: all package question responses (the @foo@ template variables)
//   - Package: package metadata (name, version, repo, image, description)
//   - System: system-level information (hostname, IPs; extensible facility)
//   - Dep: installed dependencies keyed by dep key (e.g. .Dep.db.Host,
//     {{index .Dep.db.Ports "sql"}}). The map is nil for packages with no deps.
type TemplateData struct {
	Responses Responses              // All question responses (accessible as .Responses.key)
	Package   TemplatePackageInfo    // Package metadata (accessible as .Package.Name, etc.)
	System    TemplateSystemInfo     // System info (accessible as .System.Hostname, etc.)
	Dep       map[string]TemplateDep // Dependencies by key (accessible as .Dep.KEY.Host / .Dep.KEY.Ports)
}

// ExecuteTemplate parses and executes a Go text/template string with the
// provided TemplateData. Returns the rendered output or a parse/execution error.
//
// The content parameter is a Go text/template string. Template variables are
// accessed via the TemplateData structure:
//   - {{.Responses.key}} for question response values
//   - {{.Package.Name}} for package metadata
//   - {{.System.Hostname}} for system information
//   - {{.Dep.key.Host}} / {{index .Dep.key.Ports "sql"}} for dependency coords
func ExecuteTemplate(name, content string, data TemplateData) (string, error) {
	tmpl, err := template.New(name).Parse(content)
	if err != nil {
		return "", fmt.Errorf("parse template %q: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %q: %w", name, err)
	}

	return buf.String(), nil
}

// ApplyPackageTemplates renders all package templates and writes them to the
// appropriate volume paths. The volumePathFn maps a volume name to its
// absolute directory path on the host filesystem.
//
// For each template, the target file path is constructed as
// volumePathFn(template.Volume) + "/" + template.Path. If the target file
// already exists, it is not overwritten (preserving data from archive uploads
// or previous runs). Parent directories are created as needed.
//
// Parameters:
//   - templates: compiled package templates keyed by name
//   - data: template data context with responses, package info, and system info
//   - volumePathFn: function that resolves a volume name to its host directory
func ApplyPackageTemplates(templates map[string]PackageTemplate, data TemplateData, volumePathFn func(volName string) string) error {
	for name, tmpl := range templates {
		volDir := volumePathFn(tmpl.Volume)
		targetPath := filepath.Join(volDir, tmpl.Path)

		// Do not overwrite existing files (e.g., from archive upload).
		if _, err := os.Stat(targetPath); err == nil {
			continue
		}

		rendered, err := ExecuteTemplate(name, tmpl.Content, data)
		if err != nil {
			return fmt.Errorf("template %q: %w", name, err)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0750); err != nil {
			return fmt.Errorf("template %q: create parent dirs: %w", name, err)
		}

		if err := os.WriteFile(targetPath, []byte(rendered), 0600); err != nil {
			return fmt.Errorf("template %q: write file: %w", name, err)
		}
	}

	return nil
}
