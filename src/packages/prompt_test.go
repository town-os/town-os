package packages

import "testing"

func TestOutput(t *testing.T) {
	table := map[string]struct {
		output   OutputType
		input    string
		expected any
		err      bool
	}{
		"port_good": {
			output:   Port,
			input:    "80",
			expected: uint16(80),
			err:      false,
		},
		"port_bad": {
			output:   Port,
			input:    "8675309",
			expected: 0,
			err:      true,
		},
		"port_signed": {
			output:   Port,
			input:    "-80",
			expected: 0,
			err:      true,
		},
		"hostname_good": {
			output:   Hostname,
			input:    "hostname",
			expected: "hostname",
			err:      false,
		},
		"hostname_integer_first": {
			output:   Hostname,
			input:    "9hostname",
			expected: "",
			err:      true,
		},
		"domainname (bad)": {
			output:   Hostname,
			input:    "hostname.io",
			expected: "",
			err:      true,
		},
	}

	for name, item := range table {
		ex, err := item.output.Output(item.input)

		if item.err {
			if err == nil {
				t.Fatalf("error expected but not received: %q", name)
			}
		} else if item.expected != ex {
			t.Fatalf("expected value was not equal: %q: expected: %v, actual: %v", name, item.expected, ex)
		}
	}
}
