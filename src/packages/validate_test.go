package packages

import (
	"errors"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestNormalizeImageURL(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"single name no tag":                {"nginx", "docker.io/library/nginx:latest"},
		"single name with tag":              {"nginx:1.25", "docker.io/library/nginx:1.25"},
		"single name with latest":           {"nginx:latest", "docker.io/library/nginx:latest"},
		"two components no tag":             {"myuser/myapp", "docker.io/myuser/myapp:latest"},
		"two components with tag":           {"myuser/myapp:v2", "docker.io/myuser/myapp:v2"},
		"full url no tag":                   {"ghcr.io/user/app", "ghcr.io/user/app:latest"},
		"full url with tag":                 {"ghcr.io/user/app:v1", "ghcr.io/user/app:v1"},
		"full url with digest":              {"ghcr.io/user/app:sha256", "ghcr.io/user/app:sha256"},
		"docker hub explicit no tag":        {"docker.io/library/nginx", "docker.io/library/nginx:latest"},
		"docker hub explicit with tag":      {"docker.io/library/nginx:alpine", "docker.io/library/nginx:alpine"},
		"registry with port no tag":         {"registry.example.com/myapp", "registry.example.com/myapp:latest"},
		"registry with port with tag":       {"registry.example.com/myapp:v1", "registry.example.com/myapp:v1"},
		"deep path":                         {"ghcr.io/org/sub/image", "ghcr.io/org/sub/image:latest"},
		"deep path with tag":                {"ghcr.io/org/sub/image:v3", "ghcr.io/org/sub/image:v3"},
		"empty string":                      {"", ""},
		"alpine":                            {"alpine", "docker.io/library/alpine:latest"},
		"redis with alpine tag":             {"redis:7.0-alpine", "docker.io/library/redis:7.0-alpine"},
		"quay registry":                     {"quay.io/prometheus/node-exporter:v1.5.0", "quay.io/prometheus/node-exporter:v1.5.0"},
		"localhost namespace":               {"localhost/myimage", "docker.io/localhost/myimage:latest"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := NormalizeImageURL(tt.input)
			if got != tt.want {
				t.Fatalf("NormalizeImageURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateImageURL(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		err := ValidateImageURL("nginx:latest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		err := ValidateImageURL("")
		if err == nil {
			t.Fatal("expected error for empty image")
		}
		if !errors.Is(err, ErrInvalidImage) {
			t.Fatalf("expected ErrInvalidImage, got %v", err)
		}
	})
}

func TestValidateEnvironmentKey(t *testing.T) {
	valid := []string{"PATH", "HOME", "NGINX_HOST", "a", "_private", "_A1", "lower_case"}
	for _, key := range valid {
		t.Run("valid/"+key, func(t *testing.T) {
			err := ValidateEnvironmentKey(key)
			if err != nil {
				t.Fatalf("expected %q to be valid: %v", key, err)
			}
		})
	}

	invalid := []string{
		"",
		"1BAD",
		"-DASH",
		"HAS SPACE",
		"has.dot",
		"has-dash",
		"bad!",
		"foo=bar",
	}
	for _, key := range invalid {
		name := key
		if name == "" {
			name = "empty"
		}
		t.Run("invalid/"+name, func(t *testing.T) {
			err := ValidateEnvironmentKey(key)
			if err == nil {
				t.Fatalf("expected %q to be invalid", key)
			}
			if !errors.Is(err, ErrInvalidEnvironmentKey) {
				t.Fatalf("expected ErrInvalidEnvironmentKey, got %v", err)
			}
		})
	}
}

func TestValidateQuestionName(t *testing.T) {
	valid := []string{"hostname", "port", "password", "myVar123", "abc", "A", "x1"}
	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			err := ValidateQuestionName(name)
			if err != nil {
				t.Fatalf("expected %q to be valid: %v", name, err)
			}
		})
	}

	invalid := []string{
		"",
		"has-dash",
		"has_underscore",
		"has space",
		"has.dot",
		"has@at",
		"123!",
	}
	for _, name := range invalid {
		label := name
		if label == "" {
			label = "empty"
		}
		t.Run("invalid/"+label, func(t *testing.T) {
			err := ValidateQuestionName(name)
			if err == nil {
				t.Fatalf("expected %q to be invalid", name)
			}
			if !errors.Is(err, ErrInvalidQuestionName) {
				t.Fatalf("expected ErrInvalidQuestionName, got %v", err)
			}
		})
	}
}

func TestValidateMountpoint(t *testing.T) {
	valid := []string{"/", "/data", "/usr/share/nginx/html", "/mnt/mydata"}
	for _, mp := range valid {
		t.Run("valid/"+mp, func(t *testing.T) {
			err := ValidateMountpoint(mp)
			if err != nil {
				t.Fatalf("expected %q to be valid: %v", mp, err)
			}
		})
	}

	invalid := []string{"", "relative/path", "data", "./relative", "../parent"}
	for _, mp := range invalid {
		label := mp
		if label == "" {
			label = "empty"
		}
		t.Run("invalid/"+label, func(t *testing.T) {
			err := ValidateMountpoint(mp)
			if err == nil {
				t.Fatalf("expected %q to be invalid", mp)
			}
			if !errors.Is(err, ErrInvalidMountpoint) {
				t.Fatalf("expected ErrInvalidMountpoint, got %v", err)
			}
		})
	}
}

func TestValidateVolumeName(t *testing.T) {
	valid := []string{"data", "my-volume", "my_volume", "Volume1", "a123", "config.d"}
	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			err := ValidateVolumeName(name)
			if err != nil {
				t.Fatalf("expected %q to be valid: %v", name, err)
			}
		})
	}

	invalid := []string{"", "-start", ".hidden", "_start", "has space", "bad!", "slashes/bad"}
	for _, name := range invalid {
		label := name
		if label == "" {
			label = "empty"
		}
		t.Run("invalid/"+label, func(t *testing.T) {
			err := ValidateVolumeName(name)
			if err == nil {
				t.Fatalf("expected %q to be invalid", name)
			}
			if !errors.Is(err, ErrInvalidVolumeName) {
				t.Fatalf("expected ErrInvalidVolumeName, got %v", err)
			}
		})
	}
}

func TestValidateArchiveSpec(t *testing.T) {
	volumes := map[string]InputPackageVolume{
		"data": {Mountpoint: "/data"},
	}

	t.Run("valid spec", func(t *testing.T) {
		spec := InputPackageArchive{Image: "nginx:latest", Directory: "/usr/share/nginx/html", Volume: "data"}
		if err := ValidateArchiveSpec(spec, volumes); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing image", func(t *testing.T) {
		spec := InputPackageArchive{Image: "", Directory: "/data", Volume: "data"}
		err := ValidateArchiveSpec(spec, volumes)
		if err == nil {
			t.Fatal("expected error for missing image")
		}
		if !errors.Is(err, ErrInvalidArchiveSpec) {
			t.Fatalf("expected ErrInvalidArchiveSpec, got %v", err)
		}
	})

	t.Run("non-absolute directory", func(t *testing.T) {
		spec := InputPackageArchive{Image: "nginx:latest", Directory: "relative/path", Volume: "data"}
		err := ValidateArchiveSpec(spec, volumes)
		if err == nil {
			t.Fatal("expected error for non-absolute directory")
		}
		if !errors.Is(err, ErrInvalidArchiveSpec) {
			t.Fatalf("expected ErrInvalidArchiveSpec, got %v", err)
		}
	})

	t.Run("unknown volume", func(t *testing.T) {
		spec := InputPackageArchive{Image: "nginx:latest", Directory: "/data", Volume: "nonexistent"}
		err := ValidateArchiveSpec(spec, volumes)
		if err == nil {
			t.Fatal("expected error for unknown volume")
		}
		if !errors.Is(err, ErrInvalidArchiveSpec) {
			t.Fatalf("expected ErrInvalidArchiveSpec, got %v", err)
		}
	})
}

func TestValidateGitURL(t *testing.T) {
	t.Run("valid HTTPS URL", func(t *testing.T) {
		err := ValidateGitURL("https://github.com/example/repo.git")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty string accepted", func(t *testing.T) {
		err := ValidateGitURL("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing scheme rejected", func(t *testing.T) {
		err := ValidateGitURL("github.com/example/repo.git")
		if err == nil {
			t.Fatal("expected error for missing scheme")
		}
		if !errors.Is(err, ErrInvalidGitURL) {
			t.Fatalf("expected ErrInvalidGitURL, got %v", err)
		}
	})

	t.Run("bare string rejected", func(t *testing.T) {
		err := ValidateGitURL("not-a-url")
		if err == nil {
			t.Fatal("expected error for bare string")
		}
		if !errors.Is(err, ErrInvalidGitURL) {
			t.Fatalf("expected ErrInvalidGitURL, got %v", err)
		}
	})

	t.Run("file URL accepted", func(t *testing.T) {
		err := ValidateGitURL("file:///tmp/repo.git")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("SSH URL accepted", func(t *testing.T) {
		err := ValidateGitURL("ssh://git@github.com/example/repo.git")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("HTTP URL accepted", func(t *testing.T) {
		err := ValidateGitURL("http://github.com/example/repo.git")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("HTTPS with credentials accepted", func(t *testing.T) {
		err := ValidateGitURL("https://user:pass@github.com/example/repo.git")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("scheme only rejected", func(t *testing.T) {
		err := ValidateGitURL("https://")
		if err == nil {
			t.Fatal("expected error for scheme-only URL")
		}
		if !errors.Is(err, ErrInvalidGitURL) {
			t.Fatalf("expected ErrInvalidGitURL, got %v", err)
		}
	})
}

func TestValidateDoesNotCheckGitURLs(t *testing.T) {
	// Validate() does not check git URLs — that happens in Compile().
	// This verifies the boundary: invalid git URL passes Validate but fails Compile.
	pkg := InputPackage{
		Image:       "nginx",
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{"config": {Mountpoint: "/config", Git: "not-a-url"}},
		Questions:   map[string]Question{},
	}
	err := pkg.Validate()
	if err != nil {
		t.Fatalf("Validate should accept invalid git URL, got: %v", err)
	}

	_, err = pkg.Compile(Responses{})
	if err == nil {
		t.Fatal("Compile should reject invalid git URL")
	}
	if !errors.Is(err, ErrInvalidGitURL) {
		t.Fatalf("expected ErrInvalidGitURL, got %v", err)
	}
}

func TestYAMLVolumeGitParsing(t *testing.T) {
	t.Run("YAML with git field", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /config
    git: https://github.com/example/config.git
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Volumes["config"].Git != "https://github.com/example/config.git" {
			t.Fatalf("expected git URL, got %q", pkg.Volumes["config"].Git)
		}
	})

	t.Run("YAML without git field", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /data
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Volumes["data"].Git != "" {
			t.Fatalf("expected empty git, got %q", pkg.Volumes["data"].Git)
		}
	})

	t.Run("compile from YAML with git", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /config
    git: https://github.com/example/config.git
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		compiled, err := pkg.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected compile error: %v", err)
		}
		if compiled.Volumes["config"].Git != "https://github.com/example/config.git" {
			t.Fatalf("expected git URL in compiled output, got %q", compiled.Volumes["config"].Git)
		}
	})
}

func TestCompileNormalizesImage(t *testing.T) {
	tests := map[string]struct {
		image string
		want  string
	}{
		"single name":     {"nginx", "docker.io/library/nginx:latest"},
		"two components":  {"myuser/myapp", "docker.io/myuser/myapp:latest"},
		"full with tag":   {"ghcr.io/user/app:v1", "ghcr.io/user/app:v1"},
		"already full":    {"docker.io/library/nginx:latest", "docker.io/library/nginx:latest"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			input := InputPackage{
				Image:       tt.image,
				Environment: map[string]string{},
				Network:     InputPackageNetwork{},
				Volumes:     map[string]InputPackageVolume{},
				Questions:   map[string]Question{},
			}
			p, err := input.Compile(Responses{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Image != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, p.Image)
			}
		})
	}
}

func TestCompileRejectsInvalidQuestionName(t *testing.T) {
	input := InputPackage{
		Image:       "nginx",
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{},
		Questions:   map[string]Question{"bad-name": {Query: "test?"}},
	}
	_, err := input.Compile(Responses{})
	if err == nil {
		t.Fatal("expected error for invalid question name")
	}
	if !errors.Is(err, ErrInvalidQuestionName) {
		t.Fatalf("expected ErrInvalidQuestionName, got %v", err)
	}
}

func TestCompileRejectsInvalidEnvironmentKey(t *testing.T) {
	input := InputPackage{
		Image:       "nginx",
		Environment: map[string]string{"1BAD": "value"},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{},
		Questions:   map[string]Question{},
	}
	_, err := input.Compile(Responses{})
	if err == nil {
		t.Fatal("expected error for invalid environment key")
	}
	if !errors.Is(err, ErrInvalidEnvironmentKey) {
		t.Fatalf("expected ErrInvalidEnvironmentKey, got %v", err)
	}
}

func TestCompileRejectsInvalidVolumeName(t *testing.T) {
	input := InputPackage{
		Image:       "nginx",
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{"bad name!": {Mountpoint: "/data"}},
		Questions:   map[string]Question{},
	}
	_, err := input.Compile(Responses{})
	if err == nil {
		t.Fatal("expected error for invalid volume name")
	}
	if !errors.Is(err, ErrInvalidVolumeName) {
		t.Fatalf("expected ErrInvalidVolumeName, got %v", err)
	}
}

func TestCompileRejectsInvalidMountpoint(t *testing.T) {
	input := InputPackage{
		Image:       "nginx",
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "relative/path"}},
		Questions:   map[string]Question{},
	}
	_, err := input.Compile(Responses{})
	if err == nil {
		t.Fatal("expected error for relative mountpoint")
	}
	if !errors.Is(err, ErrInvalidMountpoint) {
		t.Fatalf("expected ErrInvalidMountpoint, got %v", err)
	}
}

func TestCompileRejectsEmptyImage(t *testing.T) {
	input := InputPackage{
		Image:       "",
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{},
		Questions:   map[string]Question{},
	}
	_, err := input.Compile(Responses{})
	if err == nil {
		t.Fatal("expected error for empty image")
	}
	if !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("expected ErrInvalidImage, got %v", err)
	}
}

func TestCompileAcceptsTemplateMountpoint(t *testing.T) {
	// Template variables in mountpoints should be accepted during Validate(),
	// then validated after substitution.
	input := InputPackage{
		Image:       "nginx",
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/@path@/files"}},
		Questions:   map[string]Question{"path": {Query: "Mount path?"}},
	}
	p, err := input.Compile(Responses{"path": "mnt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Volumes["data"].Mountpoint != "/mnt/files" {
		t.Fatalf("expected /mnt/files, got %s", p.Volumes["data"].Mountpoint)
	}
}

func TestCompileRejectsTemplatedMountpointThatResolvesToRelative(t *testing.T) {
	// If a template resolves such that the mountpoint no longer starts with /,
	// it should be rejected.
	input := InputPackage{
		Image:       "nginx",
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "@path@/files"}},
		Questions:   map[string]Question{"path": {Query: "Mount path?"}},
	}
	_, err := input.Compile(Responses{"path": "relative"})
	if err == nil {
		t.Fatal("expected error for mountpoint that resolves to relative path")
	}
}

// TestYAMLIntegerParsing verifies that YAML integer values are correctly
// unmarshaled into map[string]string fields (environment, network ports).
func TestYAMLIntegerParsing(t *testing.T) {
	t.Run("environment integer value", func(t *testing.T) {
		input := `
image: nginx
environment:
  SOME_PORT: 8080
  WORKERS: 4
network:
  external: {}
  internal: {}
volumes: {}
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Environment["SOME_PORT"] != "8080" {
			t.Fatalf("expected SOME_PORT=8080, got %q", pkg.Environment["SOME_PORT"])
		}
		if pkg.Environment["WORKERS"] != "4" {
			t.Fatalf("expected WORKERS=4, got %q", pkg.Environment["WORKERS"])
		}
	})

	t.Run("network port integer keys and values", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external:
    80: 8080
  internal:
    6379: 6379
volumes: {}
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Network.External["80"] != "8080" {
			t.Fatalf("expected external[80]=8080, got %q", pkg.Network.External["80"])
		}
		if pkg.Network.Internal["6379"] != "6379" {
			t.Fatalf("expected internal[6379]=6379, got %q", pkg.Network.Internal["6379"])
		}
	})

	t.Run("boolean YAML values in environment", func(t *testing.T) {
		input := `
image: nginx
environment:
  DEBUG: true
  VERBOSE: false
network:
  external: {}
  internal: {}
volumes: {}
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Environment["DEBUG"] != "true" {
			t.Fatalf("expected DEBUG=true, got %q", pkg.Environment["DEBUG"])
		}
		if pkg.Environment["VERBOSE"] != "false" {
			t.Fatalf("expected VERBOSE=false, got %q", pkg.Environment["VERBOSE"])
		}
	})

	t.Run("float YAML value in environment", func(t *testing.T) {
		input := `
image: nginx
environment:
  RATIO: 3.14
network:
  external: {}
  internal: {}
volumes: {}
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Environment["RATIO"] != "3.14" {
			t.Fatalf("expected RATIO=3.14, got %q", pkg.Environment["RATIO"])
		}
	})

	t.Run("compile with integer environment values", func(t *testing.T) {
		input := `
image: nginx
environment:
  WORKERS: 4
  PORT: 8080
network:
  external:
    80: 80
  internal: {}
volumes: {}
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		compiled, err := pkg.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected compile error: %v", err)
		}
		if compiled.Environment["WORKERS"] != "4" {
			t.Fatalf("expected WORKERS=4, got %q", compiled.Environment["WORKERS"])
		}
		if compiled.Environment["PORT"] != "8080" {
			t.Fatalf("expected PORT=8080, got %q", compiled.Environment["PORT"])
		}
	})
}

func TestValidateInputPackage(t *testing.T) {
	t.Run("valid package", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "nginx:latest",
			Environment: map[string]string{"NGINX_HOST": "localhost"},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data"}},
			Questions:   map[string]Question{"hostname": {Query: "hostname?", Type: Hostname}},
		}
		err := pkg.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty package minimal", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "alpine",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		err := pkg.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("template in mountpoint skips mountpoint validation", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "nginx",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "@prefix@/data"}},
			Questions:   map[string]Question{"prefix": {Query: "prefix?"}},
		}
		err := pkg.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects empty image", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		err := pkg.Validate()
		if !errors.Is(err, ErrInvalidImage) {
			t.Fatalf("expected ErrInvalidImage, got %v", err)
		}
	})

	t.Run("rejects invalid environment key", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "nginx",
			Environment: map[string]string{"bad-key": "val"},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		err := pkg.Validate()
		if !errors.Is(err, ErrInvalidEnvironmentKey) {
			t.Fatalf("expected ErrInvalidEnvironmentKey, got %v", err)
		}
	})

	t.Run("rejects invalid question name", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "nginx",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"bad_name": {Query: "test?"}},
		}
		err := pkg.Validate()
		if !errors.Is(err, ErrInvalidQuestionName) {
			t.Fatalf("expected ErrInvalidQuestionName, got %v", err)
		}
	})

	t.Run("rejects invalid volume name", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "nginx",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{".hidden": {Mountpoint: "/data"}},
			Questions:   map[string]Question{},
		}
		err := pkg.Validate()
		if !errors.Is(err, ErrInvalidVolumeName) {
			t.Fatalf("expected ErrInvalidVolumeName, got %v", err)
		}
	})

	t.Run("rejects invalid mountpoint", func(t *testing.T) {
		pkg := InputPackage{
			Image:       "nginx",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "no-slash"}},
			Questions:   map[string]Question{},
		}
		err := pkg.Validate()
		if !errors.Is(err, ErrInvalidMountpoint) {
			t.Fatalf("expected ErrInvalidMountpoint, got %v", err)
		}
	})
}

func TestCompileDoesNotTemplateImage(t *testing.T) {
	input := InputPackage{
		Image:       "debian:@tag@",
		Environment: map[string]string{"TAG": "@tag@"},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{},
		Questions:   map[string]Question{"tag": {Query: "Tag?"}},
	}
	p, err := input.Compile(Responses{"tag": "latest"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Image should NOT be substituted — template chars are literal.
	if p.Image != "docker.io/library/debian:@tag@" {
		t.Fatalf("expected docker.io/library/debian:@tag@, got %s", p.Image)
	}
	// Environment SHOULD be substituted.
	if p.Environment["TAG"] != "latest" {
		t.Fatalf("expected TAG=latest, got %s", p.Environment["TAG"])
	}
}

func TestValidateImageURLAcceptsTemplateChars(t *testing.T) {
	err := ValidateImageURL("debian:@tag@")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestYAMLVolumeQuotaParsing(t *testing.T) {
	t.Run("string quota", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /data
    quota: 1gb
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Volumes["data"].Quota != "1gb" {
			t.Fatalf("expected quota 1gb, got %q", pkg.Volumes["data"].Quota)
		}
		if pkg.Volumes["data"].Mountpoint != "/data" {
			t.Fatalf("expected mountpoint /data, got %q", pkg.Volumes["data"].Mountpoint)
		}
	})

	t.Run("integer quota in YAML", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /data
    quota: 1073741824
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Volumes["data"].Quota != "1073741824" {
			t.Fatalf("expected quota 1073741824, got %q", pkg.Volumes["data"].Quota)
		}
	})

	t.Run("no quota omitted", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /data
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Volumes["data"].Quota != "" {
			t.Fatalf("expected empty quota, got %q", pkg.Volumes["data"].Quota)
		}
	})

	t.Run("compile YAML with quota", func(t *testing.T) {
		input := `
image: nginx
environment: {}
network:
  external: {}
  internal: {}
volumes:
  data:
    mountpoint: /data
    quota: 2gb
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		compiled, err := pkg.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected compile error: %v", err)
		}
		if compiled.Volumes["data"].Quota != 2147483648 {
			t.Fatalf("expected 2147483648, got %d", compiled.Volumes["data"].Quota)
		}
	})
}
