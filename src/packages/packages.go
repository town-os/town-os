package packages

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
	Prompts     []Prompt            `yaml:"prompt"`
}

func (i *InputPackage) Compile() (*Package, error) {
	return nil, nil
}
