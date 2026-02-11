package packages

import (
	"errors"
	"fmt"
	"strconv"
)

type (
	Responses map[string]string
	PortMap   map[uint16]uint16
)

const TemplateChar = '@'

var (
	ErrInvalidResponse = errors.New("response does not match a prompt question")
)

type PackageIdentity struct {
	Name    string
	Version string
}

type PackageVolume struct {
	Mountpoint string
}

type PackageNetwork struct {
	External PortMap
	Internal PortMap
}

type Package struct {
	Image       string
	Environment map[string]string
	Network     PackageNetwork
	Volumes     map[string]PackageVolume
}

type InputPackageNetwork struct {
	External map[string]string `yaml:"external"`
	Internal map[string]string `yaml:"internal"`
	// TODO: hostname (with validation)
}

type InputPackage struct {
	Image       string                   `yaml:"image"`
	Environment map[string]string        `yaml:"environment"`
	Network     InputPackageNetwork      `yaml:"network"`
	Volumes     map[string]PackageVolume `yaml:"volumes"`
	Questions   map[string]string        `yaml:"questions"`
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

	if inside {
		out += fmt.Sprintf("%c%s", TemplateChar, tv)
	}

	return out
}

func (i *InputPackage) iterateFields(iv, response string) {
	i.Image = applyTemplate(i.Image, iv, response)

	for k, v := range i.Environment {
		i.Environment[k] = applyTemplate(v, iv, response)
	}

	for s, d := range i.Network.External {
		i.Network.External[applyTemplate(s, iv, response)] = applyTemplate(d, iv, response)
	}

	for s, d := range i.Network.Internal {
		i.Network.Internal[applyTemplate(s, iv, response)] = applyTemplate(d, iv, response)
	}

	for name := range i.Volumes {
		pv := i.Volumes[name]
		pv.Mountpoint = applyTemplate(pv.Mountpoint, iv, response)
		i.Volumes[name] = pv
	}
}

func convert(p map[string]string) (PortMap, error) {
	pm := PortMap{}

	for forward, host := range p {
		out_f, err := strToPort(forward)
		if err != nil {
			return nil, err
		}

		out_h, err := strToPort(host)
		if err != nil {
			return nil, err
		}

		pm[out_f] = out_h
	}

	return pm, nil
}

func strToPort(input string) (uint16, error) {
	u, err := strconv.ParseUint(input, 10, 64)
	if err != nil {
		return 0, err
	}

	if u >= 65535 {
		return 0, ErrInvalidPort
	}

	return uint16(u), nil
}

func (i *InputPackage) Compile(response Responses) (*Package, error) {
	for prompt, resp := range response {
		if _, ok := i.Questions[prompt]; !ok {
			return nil, fmt.Errorf("%q: %v", prompt, ErrInvalidResponse)
		}

		i.iterateFields(prompt, resp)
	}

	external, err := convert(i.Network.External)
	if err != nil {
		return nil, err
	}

	internal, err := convert(i.Network.Internal)
	if err != nil {
		return nil, err
	}

	p := &Package{
		Image:       i.Image,
		Environment: i.Environment,
		Network:     PackageNetwork{External: external, Internal: internal},
		// TODO: validate volume mountpoints
		Volumes: i.Volumes,
	}

	return p, nil
}
