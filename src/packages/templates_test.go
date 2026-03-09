// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteTemplate(t *testing.T) {
	t.Run("basic response substitution", func(t *testing.T) {
		data := TemplateData{
			Responses: Responses{"hostname": "example.com", "port": "8080"},
		}
		result, err := ExecuteTemplate("test", "host: {{.Responses.hostname}}\nport: {{.Responses.port}}", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "host: example.com\nport: 8080"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("package info access", func(t *testing.T) {
		data := TemplateData{
			Package: TemplatePackageInfo{
				Name:        "myapp",
				Version:     "1.0",
				Repo:        "core",
				Image:       "docker.io/library/nginx:latest",
				Description: "A test application",
			},
		}
		result, err := ExecuteTemplate("test", "name={{.Package.Name}} version={{.Package.Version}} repo={{.Package.Repo}} image={{.Package.Image}} desc={{.Package.Description}}", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "name=myapp version=1.0 repo=core image=docker.io/library/nginx:latest desc=A test application"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("system info access", func(t *testing.T) {
		data := TemplateData{
			System: TemplateSystemInfo{
				Hostname:   "myhost",
				ExternalIP: "1.2.3.4",
				InternalIP: "192.168.1.10",
			},
		}
		result, err := ExecuteTemplate("test", "hostname={{.System.Hostname}} ext={{.System.ExternalIP}} int={{.System.InternalIP}}", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "hostname=myhost ext=1.2.3.4 int=192.168.1.10"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("all data combined", func(t *testing.T) {
		data := TemplateData{
			Responses: Responses{"dbpass": "secret123"},
			Package:   TemplatePackageInfo{Name: "myapp", Version: "2.0"},
			System:    TemplateSystemInfo{Hostname: "server1"},
		}
		tmpl := `app: {{.Package.Name}}
version: {{.Package.Version}}
host: {{.System.Hostname}}
db_password: {{.Responses.dbpass}}`
		result, err := ExecuteTemplate("test", tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "app: myapp\nversion: 2.0\nhost: server1\ndb_password: secret123"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("invalid template syntax", func(t *testing.T) {
		data := TemplateData{}
		_, err := ExecuteTemplate("test", "{{.Bad", data)
		if err == nil {
			t.Fatal("expected error for invalid template syntax")
		}
	})

	t.Run("missing key returns empty string", func(t *testing.T) {
		data := TemplateData{
			Responses: Responses{},
		}
		result, err := ExecuteTemplate("test", "val={{.Responses.nonexistent}}", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Go templates return <no value> for missing map keys by default.
		expected := "val=<no value>"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("empty template content", func(t *testing.T) {
		data := TemplateData{}
		result, err := ExecuteTemplate("test", "", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "" {
			t.Fatalf("expected empty, got %q", result)
		}
	})

	t.Run("conditional template", func(t *testing.T) {
		data := TemplateData{
			Responses: Responses{"debug": "true"},
		}
		tmpl := `{{if eq .Responses.debug "true"}}debug_mode: on{{else}}debug_mode: off{{end}}`
		result, err := ExecuteTemplate("test", tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "debug_mode: on" {
			t.Fatalf("expected 'debug_mode: on', got %q", result)
		}
	})

	t.Run("range over responses", func(t *testing.T) {
		data := TemplateData{
			Responses: Responses{"key1": "val1"},
		}
		tmpl := `{{range $k, $v := .Responses}}{{$k}}={{$v}}
{{end}}`
		result, err := ExecuteTemplate("test", tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "key1=val1\n"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("empty system info fields", func(t *testing.T) {
		data := TemplateData{
			System: TemplateSystemInfo{},
		}
		result, err := ExecuteTemplate("test", "h={{.System.Hostname}}", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "h=" {
			t.Fatalf("expected 'h=', got %q", result)
		}
	})
}

func TestApplyPackageTemplates(t *testing.T) {
	t.Run("writes template to volume", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"config": {
				Volume:  "data",
				Path:    "config.yaml",
				Content: "host: {{.Responses.hostname}}",
			},
		}
		data := TemplateData{
			Responses: Responses{"hostname": "example.com"},
		}

		err := ApplyPackageTemplates(templates, data, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(volDir, "config.yaml"))
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(content) != "host: example.com" {
			t.Fatalf("expected 'host: example.com', got %q", string(content))
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"nested": {
				Volume:  "data",
				Path:    "etc/app/config.yaml",
				Content: "value: {{.Responses.val}}",
			},
		}
		data := TemplateData{
			Responses: Responses{"val": "42"},
		}

		err := ApplyPackageTemplates(templates, data, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(volDir, "etc", "app", "config.yaml"))
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(content) != "value: 42" {
			t.Fatalf("expected 'value: 42', got %q", string(content))
		}
	})

	t.Run("does not overwrite existing file", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		existingContent := "existing content"
		if err := os.WriteFile(filepath.Join(volDir, "config.yaml"), []byte(existingContent), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		templates := map[string]PackageTemplate{
			"config": {
				Volume:  "data",
				Path:    "config.yaml",
				Content: "new: {{.Responses.val}}",
			},
		}
		data := TemplateData{
			Responses: Responses{"val": "overwritten"},
		}

		err := ApplyPackageTemplates(templates, data, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(volDir, "config.yaml"))
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(content) != existingContent {
			t.Fatalf("expected existing content preserved, got %q", string(content))
		}
	})

	t.Run("multiple templates", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"config": {
				Volume:  "data",
				Path:    "config.yaml",
				Content: "host: {{.Responses.host}}",
			},
			"env": {
				Volume:  "data",
				Path:    "env.sh",
				Content: "export HOST={{.Responses.host}}",
			},
		}
		data := TemplateData{
			Responses: Responses{"host": "server1"},
		}

		err := ApplyPackageTemplates(templates, data, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content1, err := os.ReadFile(filepath.Join(volDir, "config.yaml"))
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if string(content1) != "host: server1" {
			t.Fatalf("expected 'host: server1', got %q", string(content1))
		}

		content2, err := os.ReadFile(filepath.Join(volDir, "env.sh"))
		if err != nil {
			t.Fatalf("read env: %v", err)
		}
		if string(content2) != "export HOST=server1" {
			t.Fatalf("expected 'export HOST=server1', got %q", string(content2))
		}
	})

	t.Run("no templates is noop", func(t *testing.T) {
		err := ApplyPackageTemplates(nil, TemplateData{}, func(string) string { return "" })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = ApplyPackageTemplates(map[string]PackageTemplate{}, TemplateData{}, func(string) string { return "" })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid template content returns error", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"bad": {
				Volume:  "data",
				Path:    "config.yaml",
				Content: "{{.Bad",
			},
		}

		err := ApplyPackageTemplates(templates, TemplateData{}, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err == nil {
			t.Fatal("expected error for invalid template content")
		}
	})

	t.Run("uses package info in template", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"info": {
				Volume:  "data",
				Path:    "info.txt",
				Content: "{{.Package.Name}} v{{.Package.Version}} from {{.Package.Repo}}",
			},
		}
		data := TemplateData{
			Package: TemplatePackageInfo{Name: "myapp", Version: "1.0", Repo: "core"},
		}

		err := ApplyPackageTemplates(templates, data, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(volDir, "info.txt"))
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		expected := "myapp v1.0 from core"
		if string(content) != expected {
			t.Fatalf("expected %q, got %q", expected, string(content))
		}
	})

	t.Run("uses system info in template", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"sys": {
				Volume:  "data",
				Path:    "sys.txt",
				Content: "hostname={{.System.Hostname}}",
			},
		}
		data := TemplateData{
			System: TemplateSystemInfo{Hostname: "myhost"},
		}

		err := ApplyPackageTemplates(templates, data, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(volDir, "sys.txt"))
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(content) != "hostname=myhost" {
			t.Fatalf("expected 'hostname=myhost', got %q", string(content))
		}
	})

	t.Run("writes file with restricted permissions", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"cfg": {
				Volume:  "data",
				Path:    "secret.conf",
				Content: "password={{.Responses.pass}}",
			},
		}
		data := TemplateData{
			Responses: Responses{"pass": "s3cret"},
		}

		err := ApplyPackageTemplates(templates, data, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		info, err := os.Stat(filepath.Join(volDir, "secret.conf"))
		if err != nil {
			t.Fatalf("stat file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("expected permissions 0600, got %04o", perm)
		}
	})

	t.Run("creates parent dirs with restricted permissions", func(t *testing.T) {
		dir := t.TempDir()
		volDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(volDir, 0750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		templates := map[string]PackageTemplate{
			"cfg": {
				Volume:  "data",
				Path:    "sub/dir/config.yaml",
				Content: "key: value",
			},
		}

		err := ApplyPackageTemplates(templates, TemplateData{}, func(volName string) string {
			return filepath.Join(dir, volName)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		info, err := os.Stat(filepath.Join(volDir, "sub", "dir"))
		if err != nil {
			t.Fatalf("stat dir: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0750 {
			t.Fatalf("expected dir permissions 0750, got %04o", perm)
		}
	})
}
