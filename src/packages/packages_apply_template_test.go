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
		"ssh url with adjacent at signs": {
			"ssh://git@@domain@:@sshport@",
			Responses{"domain": "example.com", "sshport": "2222"},
			"ssh://git@example.com:2222",
		},
		"double at literal": {
			"@@",
			Responses{"v": "x"},
			"@@",
		},
		"triple at": {
			"@@@v@",
			Responses{"v": "X"},
			"@@X",
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
			got := applyTemplates(tt.input, tt.responses)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
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
