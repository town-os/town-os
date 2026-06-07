// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"reflect"
	"testing"
)

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
		"double at preserved":   {"@@", "", "repl", "@@"},
		"adjacent templates":    {"@a@@b@", "a", "X", "X@b@"},
		"same var twice":        {"@v@ and @v@", "v", "X", "X and X"},
		"replacement with @":    {"@v@", "v", "has@sign", "has@sign"},
		"template at start":     {"@v@ end", "v", "X", "X end"},
		"template at end":       {"start @v@", "v", "X", "start X"},
		"only template":         {"@v@", "v", "X", "X"},
		"multiple unclosed":     {"@abc", "abc", "X", "@abc"},
		"volume template":       {"/data/@vol@/files", "vol", "mydata", "/data/mydata/files"},
		"triple at variable":    {"ssh://git@@@PACKAGE_DNS@/repo", "PACKAGE_DNS", "gitea.home", "ssh://git@@gitea.home/repo"},
		"double at no match":    {"admin@@example.com", "other", "x", "admin@@example.com"},
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

func TestApplyTemplates(t *testing.T) {
	tests := map[string]struct {
		input     string
		responses Responses
		expected  string
	}{
		"basic": {
			"http://@host@:@port@",
			Responses{"host": "example.com", "port": "8080"},
			"http://example.com:8080",
		},
		"no match": {
			"@unknown@",
			Responses{"host": "x"},
			"@unknown@",
		},
		"ssh url with triple at": {
			"ssh://git@@@domain@:@sshport@",
			Responses{"domain": "example.com", "sshport": "2222"},
			"ssh://git@example.com:2222",
		},
		"double at literal": {
			"@@",
			Responses{"v": "x"},
			"@",
		},
		"triple at": {
			"@@@v@",
			Responses{"v": "X"},
			"@X",
		},
		"double at in text": {
			"user@@host",
			Responses{"v": "x"},
			"user@host",
		},
		"double at followed by non-template": {
			"@@plain",
			Responses{"plain": "x"},
			"@plain",
		},
		"plain text": {
			"no templates",
			Responses{"v": "x"},
			"no templates",
		},
		"unclosed template": {
			"@trailing",
			Responses{"trailing": "x"},
			"@trailing",
		},
		"empty responses": {
			"@v@",
			Responses{},
			"@v@",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := ApplyTemplates(tt.input, tt.responses)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestCompileWithContextPackageDNS(t *testing.T) {
	ip := InputPackage{
		Image:       InputPackageImage{URL: "test-image"},
		Description: "test",
		Environment: map[string]string{
			"MY_DNS": "@PACKAGE_DNS@",
		},
		Notes: map[string]Note{
			"dns_url": {Value: "http://@PACKAGE_DNS@/admin", Type: "url"},
		},
	}

	compiled, err := ip.CompileWithContext(Responses{}, CompileContext{
		PackageDNS: "nginx.core.home",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if compiled.Environment["MY_DNS"] != "nginx.core.home" {
		t.Fatalf("expected %q, got %q", "nginx.core.home", compiled.Environment["MY_DNS"])
	}

	if compiled.Notes["dns_url"] != "http://nginx.core.home/admin" {
		t.Fatalf("expected %q, got %q", "http://nginx.core.home/admin", compiled.Notes["dns_url"])
	}
}

func TestCompileNotesTripleAtResponseSubstitution(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "test-image"},
		Notes: map[string]Note{
			"git_url": {Value: "ssh://git@@@sshhost@:@sshport@/repo"},
		},
		Questions: map[string]Question{
			"sshhost": {Query: "SSH host?"},
			"sshport": {Query: "SSH port?"},
		},
	}

	compiled, err := ip.Compile(Responses{"sshhost": "example.com", "sshport": "2222"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "ssh://git@example.com:2222/repo"
	if compiled.Notes["git_url"] != want {
		t.Fatalf("expected %q, got %q", want, compiled.Notes["git_url"])
	}
}

func TestCompileNotesWithContextPackageDNSOnly(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "test-image"},
		Notes: map[string]Note{
			"dns_url": {Value: "http://@PACKAGE_DNS@/admin", Type: "url"},
		},
	}

	notes, err := ip.CompileNotesWithContext(Responses{}, CompileContext{
		PackageDNS: "nginx.core.home",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if notes["dns_url"] != "http://nginx.core.home/admin" {
		t.Fatalf("expected %q, got %q", "http://nginx.core.home/admin", notes["dns_url"])
	}
}

func TestCompileNotesWithContextMixed(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "test-image"},
		Notes: map[string]Note{
			"url": {Value: "http://@PACKAGE_DNS@:@port@", Type: "url"},
		},
		Questions: map[string]Question{
			"port": {Query: "Port?"},
		},
	}

	notes, err := ip.CompileNotesWithContext(Responses{"port": "8080"}, CompileContext{
		PackageDNS: "app.repo.home",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if notes["url"] != "http://app.repo.home:8080" {
		t.Fatalf("expected %q, got %q", "http://app.repo.home:8080", notes["url"])
	}
}

func TestCompileNotesWithContextAllVars(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "test-image"},
		Notes: map[string]Note{
			"ext":  {Value: "http://@LOCAL_EXTERNAL_HOST@/ext"},
			"int":  {Value: "http://@LOCAL_INTERNAL_HOST@/int"},
			"dns":  {Value: "http://@PACKAGE_DNS@/dns"},
		},
	}

	notes, err := ip.CompileNotesWithContext(Responses{}, CompileContext{
		ExternalHost: "1.2.3.4",
		InternalHost: "192.168.1.1",
		PackageDNS:   "pkg.repo.home",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if notes["ext"] != "http://1.2.3.4/ext" {
		t.Fatalf("expected %q, got %q", "http://1.2.3.4/ext", notes["ext"])
	}
	if notes["int"] != "http://192.168.1.1/int" {
		t.Fatalf("expected %q, got %q", "http://192.168.1.1/int", notes["int"])
	}
	if notes["dns"] != "http://pkg.repo.home/dns" {
		t.Fatalf("expected %q, got %q", "http://pkg.repo.home/dns", notes["dns"])
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
	t.Run("valid numeric", func(t *testing.T) {
		pm, names, keyToHost, err := convert(map[string]string{"80": "8080", "443": "8443"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := PortMap{80: 8080, 443: 8443}
		if !reflect.DeepEqual(pm, expected) {
			t.Fatalf("expected %v, got %v", expected, pm)
		}
		if names != nil {
			t.Fatalf("expected nil names for pure-numeric map, got %v", names)
		}
		if !reflect.DeepEqual(keyToHost, map[string]uint16{"80": 80, "443": 443}) {
			t.Fatalf("unexpected keyToHost: %v", keyToHost)
		}
	})

	t.Run("empty", func(t *testing.T) {
		pm, names, _, err := convert(map[string]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pm) != 0 {
			t.Fatalf("expected empty PortMap, got %v", pm)
		}
		if names != nil {
			t.Fatalf("expected nil names for empty map, got %v", names)
		}
	})

	t.Run("named form only", func(t *testing.T) {
		pm, names, _, err := convert(map[string]string{"sql": "5432"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(pm, PortMap{5432: 5432}) {
			t.Fatalf("expected host=container=5432, got %v", pm)
		}
		if !reflect.DeepEqual(names, PortNameMap{5432: "sql"}) {
			t.Fatalf("expected name sql on port 5432, got %v", names)
		}
	})

	t.Run("mixed named and numeric", func(t *testing.T) {
		pm, names, _, err := convert(map[string]string{"sql": "5432", "8080": "80"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedPM := PortMap{5432: 5432, 8080: 80}
		if !reflect.DeepEqual(pm, expectedPM) {
			t.Fatalf("expected %v, got %v", expectedPM, pm)
		}
		// Numeric-keyed entries contribute no name, so the only name
		// comes from "sql".
		if !reflect.DeepEqual(names, PortNameMap{5432: "sql"}) {
			t.Fatalf("expected name sql on port 5432, got %v", names)
		}
	})

	t.Run("name preserves case", func(t *testing.T) {
		_, names, _, err := convert(map[string]string{"MySql": "5432"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if names[5432] != "MySql" {
			t.Fatalf("expected name preserved as-written, got %q", names[5432])
		}
	})

	t.Run("invalid name with dash", func(t *testing.T) {
		_, _, _, err := convert(map[string]string{"bad-name": "80"})
		if err == nil {
			t.Fatal("expected error for name containing dash")
		}
	})

	t.Run("invalid container port", func(t *testing.T) {
		_, _, _, err := convert(map[string]string{"80": "bad"})
		if err == nil {
			t.Fatal("expected error for non-numeric container port")
		}
	})

	t.Run("zero port rejected", func(t *testing.T) {
		_, _, _, err := convert(map[string]string{"0": "80"})
		if err == nil {
			t.Fatal("expected error for port 0")
		}
	})

	t.Run("duplicate container port with different names", func(t *testing.T) {
		_, _, _, err := convert(map[string]string{"sql": "5432", "primary": "5432"})
		if err == nil {
			t.Fatal("expected error for two names mapping to same container port")
		}
	})
}

func BenchmarkApplyTemplate(b *testing.B) {
	input := "prefix @var1@ middle @var2@ suffix @var1@ end"
	for b.Loop() {
		applyTemplate(input, "var1", "replacement_value")
	}
}

func BenchmarkApplyTemplates(b *testing.B) {
	input := "ssh://git@@@domain@:@sshport@/repo @extra@ path"
	responses := Responses{
		"domain":  "example.com",
		"sshport": "2222",
		"extra":   "value",
	}
	for b.Loop() {
		ApplyTemplates(input, responses)
	}
}
