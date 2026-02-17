package packages

import (
	"errors"
	"reflect"
	"testing"
)

func TestPackageIdentityString(t *testing.T) {
	pi := PackageIdentity{Name: "nginx", Version: "2.0"}
	if got := pi.String(); got != "nginx@2.0" {
		t.Fatalf("expected %q, got %q", "nginx@2.0", got)
	}
}

func TestParsePackageIdentity(t *testing.T) {
	pi, err := ParsePackageIdentity("nginx@2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi.Name != "nginx" || pi.Version != "2.0" {
		t.Fatalf("expected nginx@2.0, got %s@%s", pi.Name, pi.Version)
	}
}

func TestParsePackageIdentityErrors(t *testing.T) {
	tests := map[string]string{
		"no @":            "nginx",
		"empty name":      "@2.0",
		"empty version":   "nginx@",
		"completely empty": "",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePackageIdentity(input)
			if err == nil {
				t.Fatalf("expected error for %q", input)
			}
			if !errors.Is(err, ErrInvalidPackageIdentity) {
				t.Fatalf("expected ErrInvalidPackageIdentity, got %v", err)
			}
		})
	}
}

func TestParsePackageIdentityMultipleAt(t *testing.T) {
	pi, err := ParsePackageIdentity("name@ver@extra")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi.Name != "name" || pi.Version != "ver@extra" {
		t.Fatalf("expected name@ver@extra, got %s@%s", pi.Name, pi.Version)
	}
}

func TestPackageIdentityRoundTrip(t *testing.T) {
	original := PackageIdentity{Name: "redis", Version: "7.0"}
	parsed, err := ParsePackageIdentity(original.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed != original {
		t.Fatalf("round-trip failed: %v != %v", parsed, original)
	}
}

func TestApplyTemplate(t *testing.T) {
	table := map[string][4]string{
		"basic":            {"this is a @template@", "template", "replacement", "this is a replacement"},
		"non-existent":     {"this is a @template@", "not-template", "replacement", "this is a @template@"},
		"two variables":    {"this is a @template@ and @another-template@", "another-template", "replacement", "this is a @template@ and replacement"},
		"invalid template": {"this is a @template", "template", "replacement", "this is a @template"},
	}

	for name, data := range table {
		t.Run(name, func(t *testing.T) {
			res := applyTemplate(data[0], data[1], data[2])

			if data[3] != res {
				t.Fatalf("output did not match: expected: %s, actual: %s", data[3], res)
			}
		})
	}
}

func TestApplyTemplateAdditional(t *testing.T) {
	table := map[string][4]string{
		"empty input":           {"", "var", "repl", ""},
		"no templates":          {"plain text", "var", "repl", "plain text"},
		"empty variable name":   {"@@", "", "repl", "repl"},
		"adjacent templates":    {"@a@@b@", "a", "X", "X@b@"},
		"same var twice":        {"@v@ and @v@", "v", "X", "X and X"},
		"replacement with @":    {"@v@", "v", "has@sign", "has@sign"},
		"template at start":     {"@v@ end", "v", "X", "X end"},
		"template at end":       {"start @v@", "v", "X", "start X"},
		"only template":         {"@v@", "v", "X", "X"},
		"multiple unclosed":     {"@abc", "abc", "X", "@abc"},
		"volume template":       {"/data/@vol@/files", "vol", "mydata", "/data/mydata/files"},
	}

	for name, data := range table {
		t.Run(name, func(t *testing.T) {
			res := applyTemplate(data[0], data[1], data[2])
			if data[3] != res {
				t.Fatalf("expected: %q, actual: %q", data[3], res)
			}
		})
	}
}

func TestStrToPort(t *testing.T) {
	tests := map[string]struct {
		input   string
		want    uint16
		wantErr error
	}{
		"valid low":    {"1", 1, nil},
		"valid mid":    {"8080", 8080, nil},
		"valid max":    {"65535", 65535, nil},
		"zero":         {"0", 0, ErrInvalidPort},
		"too high":     {"65536", 0, ErrInvalidPort},
		"way too high": {"100000", 0, ErrInvalidPort},
		"negative":     {"-1", 0, nil},    // ParseUint fails
		"non-numeric":  {"abc", 0, nil},   // ParseUint fails
		"empty":        {"", 0, nil},      // ParseUint fails
		"float":        {"80.5", 0, nil},  // ParseUint fails
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := strToPort(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if tt.want == 0 && err != nil {
				// cases where ParseUint is expected to fail
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestConvert(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		pm, err := convert(map[string]string{"80": "8080", "443": "8443"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := PortMap{80: 8080, 443: 8443}
		if !reflect.DeepEqual(pm, expected) {
			t.Fatalf("expected %v, got %v", expected, pm)
		}
	})

	t.Run("empty", func(t *testing.T) {
		pm, err := convert(map[string]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pm) != 0 {
			t.Fatalf("expected empty PortMap, got %v", pm)
		}
	})

	t.Run("invalid forward port", func(t *testing.T) {
		_, err := convert(map[string]string{"bad": "80"})
		if err == nil {
			t.Fatal("expected error for invalid forward port")
		}
	})

	t.Run("invalid host port", func(t *testing.T) {
		_, err := convert(map[string]string{"80": "bad"})
		if err == nil {
			t.Fatal("expected error for invalid host port")
		}
	})

	t.Run("zero port rejected", func(t *testing.T) {
		_, err := convert(map[string]string{"0": "80"})
		if err == nil {
			t.Fatal("expected error for port 0")
		}
	})
}

func TestPackageCompile(t *testing.T) {
	table := map[string]struct {
		input     InputPackage
		output    Package
		responses Responses
		err       bool
	}{
		"basic": {
			input: InputPackage{
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "scarlett"},
				Network:     InputPackageNetwork{External: map[string]string{"80": "80"}, Internal: map[string]string{"128": "128"}},
				Volumes:     map[string]PackageVolume{},
				Questions:   map[string]string{},
			},
			output: Package{
				// FIXME: this should expand to a full image url
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "scarlett"},
				Network:     PackageNetwork{External: PortMap{80: 80}, Internal: PortMap{128: 128}},
				Volumes:     map[string]PackageVolume{},
			},
			responses: Responses{},
			err:       false,
		},
		"basic-template": {
			input: InputPackage{
				Image:       "debian:@tag@",
				Environment: map[string]string{"HELLO": "@name@"},
				Network:     InputPackageNetwork{External: map[string]string{"@external@": "80"}, Internal: map[string]string{"128": "@internal@"}},
				Volumes:     map[string]PackageVolume{},
				Questions: map[string]string{
					"tag":      "What tag should I use?",
					"name":     "Who should I say hello to?",
					"external": "What port to forward?",
					"internal": "What port to use internally?",
				},
			},
			output: Package{
				// FIXME: this should expand to a full image url
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "scarlett"},
				Network:     PackageNetwork{External: PortMap{80: 80}, Internal: PortMap{128: 128}},
				Volumes:     map[string]PackageVolume{},
			},
			responses: Responses{
				"tag":      "latest",
				"name":     "scarlett",
				"external": "80",
				"internal": "128",
			},
			err: false,
		},
		"template-errors": {
			input: InputPackage{
				Image:       "debian:@tag@",
				Environment: map[string]string{"HELLO": "@name@"},
				Network:     InputPackageNetwork{External: map[string]string{"@external@": "80"}, Internal: map[string]string{"128": "@internal@"}},
				Volumes:     map[string]PackageVolume{},
				Questions: map[string]string{
					"tag":      "What tag should I use?",
					"name":     "Who should I say hello to?",
					"external": "What port to forward?",
					"internal": "What port to use internally?",
				},
			},
			output: Package{
				// FIXME: this should expand to a full image url
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "scarlett"},
				Network:     PackageNetwork{External: PortMap{80: 80}, Internal: PortMap{128: 128}},
				Volumes:     map[string]PackageVolume{},
			},
			responses: Responses{
				"tag":      "latest",
				"name":     "scarlett",
				"external": "-80",
				"internal": "128",
			},
			err: true,
		},
	}

	for name, data := range table {
		t.Run(name, func(t *testing.T) {
			p, err := data.input.Compile(data.responses)
			if data.err {
				if err == nil {
					t.Fatal("error was expected but not received")
				}
			} else if err != nil {
				t.Fatalf("error encountered when none was expected: %v", err)
			} else {
				if !reflect.DeepEqual(*p, data.output) {
					t.Fatalf("expected output was not equal to compiled output:\n  expected: %#v\n  actual:   %#v", data.output, *p)
				}
			}
		})
	}
}

func TestPackageCompileAdditional(t *testing.T) {
	t.Run("invalid response key", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]PackageVolume{},
			Questions:   map[string]string{"name": "What is your name?"},
		}
		_, err := input.Compile(Responses{"bogus": "value"})
		if err == nil {
			t.Fatal("expected error for unknown response key")
		}
	})

	t.Run("volume template substitution", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]PackageVolume{"data": {Mountpoint: "/mnt/@path@"}},
			Questions:   map[string]string{"path": "Mount path?"},
		}
		p, err := input.Compile(Responses{"path": "mydata"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Volumes["data"].Mountpoint != "/mnt/mydata" {
			t.Fatalf("expected /mnt/mydata, got %s", p.Volumes["data"].Mountpoint)
		}
	})

	t.Run("port 65535 valid", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{External: map[string]string{"65535": "65535"}},
			Volumes:     map[string]PackageVolume{},
			Questions:   map[string]string{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("port 65535 should be valid: %v", err)
		}
		if p.Network.External[65535] != 65535 {
			t.Fatalf("expected port 65535 mapping, got %v", p.Network.External)
		}
	})

	t.Run("port 0 rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       "debian:latest",
			Environment: map[string]string{},
			Network:     InputPackageNetwork{External: map[string]string{"0": "80"}},
			Volumes:     map[string]PackageVolume{},
			Questions:   map[string]string{},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("port 0 should be rejected")
		}
	})
}
