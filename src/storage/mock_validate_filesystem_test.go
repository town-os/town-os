// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"testing"
)

func TestValidateFilesystemName(t *testing.T) {
	valid := []string{
		"test",
		"my-volume",
		"data_dir",
		"vol.1",
		"abc/def",
		"A-Z.0-9_test",
	}

	for _, name := range valid {
		err := ValidateFilesystemName(name)
		if err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
}

func TestValidateFilesystemNameRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty name"},
		{"/test", "leading slash"},
		{"test/", "trailing slash (empty component)"},
		{"test//sub", "double slash"},
		{"..", "dotdot traversal"},
		{"test/..", "dotdot in path"},
		{"test/./sub", "dot in path"},
		{"hello world", "space in name"},
		{"test\x00vol", "null byte"},
		{"my@vol", "at sign"},
		{"my:vol", "colon"},
		{"vol*name", "asterisk"},
	}

	for _, tc := range cases {
		err := ValidateFilesystemName(tc.name)
		if err == nil {
			t.Errorf("expected %q (%s) to be invalid, got nil", tc.name, tc.desc)
		}
	}
}
