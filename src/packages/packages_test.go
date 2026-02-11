package packages

import "testing"

func TestApplyTemplate(t *testing.T) {
	table := map[string][4]string{
		"basic":         {"this is a @template@", "template", "replacement", "this is a replacement"},
		"non-existent":  {"this is a @template@", "not-template", "replacement", "this is a @template@"},
		"two variables": {"this is a @template@ and @another-template@", "another-template", "replacement", "this is a @template@ and replacement"},
	}

	for name, data := range table {
		res := applyTemplate(data[0], data[1], data[2])

		if data[3] != res {
			t.Fatalf("%s: output did not match: expected: %s, actual: %s", name, data[3], res)
		}
	}
}
