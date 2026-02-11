package packages

import (
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
		res := applyTemplate(data[0], data[1], data[2])

		if data[3] != res {
			t.Fatalf("%s: output did not match: expected: %s, actual: %s", name, data[3], res)
		}
	}
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
				Environment: map[string]string{},
				Network:     InputPackageNetwork{External: map[string]string{}, Internal: map[string]string{}},
				Volumes:     map[string]PackageVolume{},
				Questions:   map[string]string{},
			},
			output: Package{
				// FIXME: this should expand to a full image url
				Image:       "debian:latest",
				Environment: map[string]string{},
				Network:     PackageNetwork{External: PortMap{}, Internal: PortMap{}},
				Volumes:     map[string]PackageVolume{},
			},
			responses: Responses{},
			err:       false,
		},
	}

	for name, data := range table {
		p, err := data.input.Compile(data.responses)
		if data.err {
			if err == nil {
				t.Fatalf("%s: error was expected but not received", name)
			}
		}

		if !reflect.DeepEqual(*p, data.output) {
			t.Fatalf("%s: expected output was not equal to compiled output: %#v %#v", name, data.output, *p)
		}
	}
}
