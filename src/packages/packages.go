package packages

import (
	"errors"
	"fmt"
)

const TemplateChar = '@'

var (
	ErrInvalidResponse = errors.New("response does not match a prompt")
)

type PackageIdentity struct {
	Name    string
	Version string
}

type PackageVolume struct {
	Name       string
	Mountpoint string
}

type PackageNetwork struct {
	External map[uint16]uint16
	Internal map[uint16]uint16
}

type Package struct {
	Image       string
	Environment map[string]string
	Network     PackageNetwork
	Volumes     []PackageVolume
}

type InputPackageNetwork struct {
	External map[string]string `yaml:"external"`
	Internal map[string]string `yaml:"internal"`
}

type InputPackage struct {
	Image       string              `yaml:"image"`
	Environment map[string]string   `yaml:"environment"`
	Network     InputPackageNetwork `yaml:"network"`
	Volumes     []PackageVolume     `yaml:"volumes"`
	Prompts     map[string]Prompt   `yaml:"prompt"`
}

func applyTemplate(input string, v string, repl string) string {
	var inside bool
	tv := ""
	out := ""

	for x := 0; x < len(input); x++ {
		if input[x] == TemplateChar {
			if inside {
				inside = false

				if tv == v {
					out += repl
				} else {
					out += fmt.Sprintf("%c%s%c", TemplateChar, tv, TemplateChar)
				}

				tv = ""
			} else {
				inside = true
			}
		} else if inside {
			tv = string(append([]byte(tv), byte(input[x])))
		} else {
			out = string(append([]byte(out), byte(input[x])))
		}
	}

	return out
}

func (i *InputPackage) Compile(response Responses) (*Package, error) {
	for prompt, resp := range response {
		p, ok := i.Prompts[prompt]

		if !ok {
			return nil, fmt.Errorf("%q: %v", prompt, ErrInvalidResponse)
		}

		p.Type.Output(resp)
	}

	return nil, nil
}
