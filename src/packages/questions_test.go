package packages

import "testing"

func TestOutput(t *testing.T) {
	table := map[string]struct {
		output   OutputType
		input    string
		expected string
		err      bool
	}{
		// Port tests
		"port_good": {
			output:   Port,
			input:    "80",
			expected: "80",
		},
		"port_max": {
			output:   Port,
			input:    "65535",
			expected: "65535",
		},
		"port_min": {
			output:   Port,
			input:    "1",
			expected: "1",
		},
		"port_zero": {
			output: Port,
			input:  "0",
			err:    true,
		},
		"port_too_high": {
			output: Port,
			input:  "65536",
			err:    true,
		},
		"port_bad": {
			output: Port,
			input:  "8675309",
			err:    true,
		},
		"port_signed": {
			output: Port,
			input:  "-80",
			err:    true,
		},
		"port_non_numeric": {
			output: Port,
			input:  "abc",
			err:    true,
		},

		// Hostname tests
		"hostname_good": {
			output:   Hostname,
			input:    "hostname",
			expected: "hostname",
		},
		"hostname_with_dash": {
			output:   Hostname,
			input:    "my-host",
			expected: "my-host",
		},
		"hostname_with_digits": {
			output:   Hostname,
			input:    "a123",
			expected: "a123",
		},
		"hostname_uppercase_lowered": {
			output:   Hostname,
			input:    "MyHost",
			expected: "myhost",
		},
		"hostname_integer_first": {
			output: Hostname,
			input:  "9hostname",
			err:    true,
		},
		"hostname_empty": {
			output: Hostname,
			input:  "",
			err:    true,
		},
		"hostname_dash_first": {
			output: Hostname,
			input:  "-bad",
			err:    true,
		},
		"hostname_with_underscore": {
			output: Hostname,
			input:  "has_underscore",
			err:    true,
		},
		"hostname_with_space": {
			output: Hostname,
			input:  "has space",
			err:    true,
		},
		"domainname (bad)": {
			output: Hostname,
			input:  "hostname.io",
			err:    true,
		},

		// Volume tests
		"volume_good": {
			output:   Volume,
			input:    "data",
			expected: "data",
		},
		"volume_with_dash": {
			output:   Volume,
			input:    "my-volume",
			expected: "my-volume",
		},
		"volume_with_underscore": {
			output:   Volume,
			input:    "my_volume",
			expected: "my_volume",
		},
		"volume_uppercase": {
			output:   Volume,
			input:    "Volume1",
			expected: "Volume1",
		},
		"volume_digits_only": {
			output:   Volume,
			input:    "123",
			expected: "123",
		},
		"volume_empty": {
			output: Volume,
			input:  "",
			err:    true,
		},
		"volume_with_space": {
			output: Volume,
			input:  "has space",
			err:    true,
		},
		"volume_special_chars": {
			output: Volume,
			input:  "bad!",
			err:    true,
		},
		"volume_with_slash": {
			output: Volume,
			input:  "slashes/bad",
			err:    true,
		},

		// Bytes tests
		"bytes_pure_integer": {
			output:   Bytes,
			input:    "1073741824",
			expected: "1073741824",
		},
		"bytes_gb": {
			output:   Bytes,
			input:    "1gb",
			expected: "1073741824",
		},
		"bytes_GB_uppercase": {
			output:   Bytes,
			input:    "2GB",
			expected: "2147483648",
		},
		"bytes_mb": {
			output:   Bytes,
			input:    "500mb",
			expected: "524288000",
		},
		"bytes_MB_uppercase": {
			output:   Bytes,
			input:    "100MB",
			expected: "104857600",
		},
		"bytes_tb": {
			output:   Bytes,
			input:    "1tb",
			expected: "1099511627776",
		},
		"bytes_TB_uppercase": {
			output:   Bytes,
			input:    "2TB",
			expected: "2199023255552",
		},
		"bytes_zero": {
			output:   Bytes,
			input:    "0",
			expected: "0",
		},
		"bytes_empty": {
			output:   Bytes,
			input:    "",
			expected: "0",
		},
		"bytes_invalid_suffix": {
			output: Bytes,
			input:  "1xyz",
			err:    true,
		},
		"bytes_negative": {
			output: Bytes,
			input:  "-1gb",
			err:    true,
		},
		"bytes_non_numeric": {
			output: Bytes,
			input:  "abc",
			err:    true,
		},
		"bytes_float": {
			output: Bytes,
			input:  "1.5gb",
			err:    true,
		},

		// Invalid output type
		"invalid_type": {
			output: OutputType("bogus"),
			input:  "anything",
			err:    true,
		},
	}

	for name, item := range table {
		t.Run(name, func(t *testing.T) {
			ex, err := item.output.Output(item.input)

			if item.err {
				if err == nil {
					t.Fatal("error expected but not received")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if item.expected != ex {
					t.Fatalf("expected: %v, actual: %v", item.expected, ex)
				}
			}
		})
	}
}
