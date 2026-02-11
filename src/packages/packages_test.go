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
				Image:       "debian:latest",
				Environment: map[string]string{"HELLO": "@name@"},
				Network:     InputPackageNetwork{External: map[string]string{"@external@": "80"}, Internal: map[string]string{"128": "@internal@"}},
				Volumes:     map[string]PackageVolume{},
				Questions: map[string]string{
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
				"name":     "scarlett",
				"external": "80",
				"internal": "128",
			},
			err: false,
		},
	}

	for name, data := range table {
		p, err := data.input.Compile(data.responses)
		if data.err {
			if err == nil {
				t.Fatalf("%s: error was expected but not received", name)
			}
		} else if err != nil {
			t.Fatalf("%s: error encountered when none was expected: %v", name, err)
		} else {
			if !reflect.DeepEqual(*p, data.output) {
				t.Fatalf("%s: expected output was not equal to compiled output: %#v %#v", name, data.output, *p)
			}
		}
	}
}
