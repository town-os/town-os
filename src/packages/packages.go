package packages

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"go.yaml.in/yaml/v4"
)

type (
	Responses map[string]string
	PortMap   map[uint16]uint16
)

const TemplateChar = '@'

var (
	ErrInvalidResponse     = errors.New("response does not match a prompt question")
	ErrInvalidResponseType = errors.New("response does not match the expected type")
	ErrMissingResponse     = errors.New("question has no response")
	ErrEmptyResponse       = errors.New("empty response")
	ErrInvalidGitSource    = errors.New("invalid git source")
	ErrMixedRuntime        = errors.New("package must specify either image (container) or vm, not both")
	ErrNoRuntime           = errors.New("package must specify either image (container) or vm")
	ErrInvalidVMConfig     = errors.New("invalid vm configuration")
)

// RuntimeType indicates whether a package runs as a container or a QEMU VM.
type RuntimeType string

const (
	// RuntimeContainer is the default runtime type using podman containers.
	RuntimeContainer RuntimeType = "container"
	// RuntimeVM runs the package as a QEMU virtual machine.
	RuntimeVM RuntimeType = "vm"
)

// InputPackageVM holds VM-specific configuration for QEMU-based packages.
type InputPackageVM struct {
	// Image is a URL or local path to the VM disk image. When a URL is
	// provided, the image is downloaded and cached to local storage.
	Image string `yaml:"image" json:"image"`
	// Memory is the amount of RAM allocated to the VM (e.g. "2gb", "512mb").
	// Defaults to "1gb" when empty.
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
	// CPUs is the number of virtual CPUs. Defaults to 1.
	CPUs int `yaml:"cpus,omitempty" json:"cpus,omitempty"`
}

// PackageVM holds compiled VM configuration after template substitution and
// parsing of human-readable sizes.
type PackageVM struct {
	// Image is the resolved VM disk image path or URL.
	Image string `json:"image"`
	// Memory is the VM memory in bytes.
	Memory uint64 `json:"memory"`
	// CPUs is the number of virtual CPUs.
	CPUs int `json:"cpus"`
}

type NoteType string

const (
	NoteURL   NoteType = "url"
	NotePhone NoteType = "phone"
	NoteEmail NoteType = "email"
)

type Note struct {
	Value string   `json:"value" yaml:"value"`
	Type  NoteType `json:"type,omitempty" yaml:"type,omitempty"`
}

var (
	ErrInvalidNoteURL   = errors.New("invalid note URL")
	ErrInvalidNotePhone = errors.New("invalid note phone number")
	ErrInvalidNoteEmail = errors.New("invalid note email address")

	notePhoneRegexp = regexp.MustCompile(`^\+?[0-9][0-9 .()\-]*$`)
	noteEmailRegexp = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

// ValidateNote validates a compiled note value against its type.
// Empty or unknown types pass through without validation.
func ValidateNote(value string, typ NoteType) error {
	switch typ {
	case NoteURL:
		if _, err := url.Parse(value); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidNoteURL, err)
		}
	case NotePhone:
		if !notePhoneRegexp.MatchString(value) {
			return fmt.Errorf("%w: %q", ErrInvalidNotePhone, value)
		}
	case NoteEmail:
		if !noteEmailRegexp.MatchString(value) {
			return fmt.Errorf("%w: %q", ErrInvalidNoteEmail, value)
		}
	}
	return nil
}

// ResponseValidationError describes a single response that failed validation.
type ResponseValidationError struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// ValidationError collects all per-response validation errors from Compile.
type ValidationError struct {
	Errors []ResponseValidationError `json:"validation_errors"`
}

func (v *ValidationError) Error() string {
	return fmt.Sprintf("%d response validation error(s)", len(v.Errors))
}

type PackageIdentity struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (p PackageIdentity) String() string {
	if p.Repo == "" {
		return fmt.Sprintf("%s@%s", p.Name, p.Version)
	}
	return fmt.Sprintf("%s/%s@%s", p.Repo, p.Name, p.Version)
}

var ErrInvalidPackageIdentity = errors.New("invalid package identity: expected repo/name@version")

func ParsePackageIdentity(s string) (PackageIdentity, error) {
	// Try repo/name@version first.
	if slashIdx := strings.IndexByte(s, '/'); slashIdx > 0 {
		repo := s[:slashIdx]
		rest := s[slashIdx+1:]
		parts := strings.SplitN(rest, "@", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return PackageIdentity{Repo: repo, Name: parts[0], Version: parts[1]}, nil
		}
		return PackageIdentity{}, ErrInvalidPackageIdentity
	}

	// Legacy fallback: name@version with empty Repo (no slash in input).
	parts := strings.SplitN(s, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return PackageIdentity{}, ErrInvalidPackageIdentity
	}
	return PackageIdentity{Name: parts[0], Version: parts[1]}, nil
}

type InputPackageVolume struct {
	Mountpoint string  `yaml:"mountpoint"`
	Quota      string  `yaml:"quota,omitempty"`
	Archive    string  `yaml:"archive,omitempty"`
	Git        string  `yaml:"git,omitempty"`
	UID        *uint32 `yaml:"uid,omitempty"`
	GID        *uint32 `yaml:"gid,omitempty"`
}

type PackageVolume struct {
	Mountpoint string  `json:"mountpoint"`
	Quota      uint64  `json:"quota,omitempty"`
	Archive    string  `json:"archive,omitempty"`
	Git        string  `json:"git,omitempty"`
	UID        *uint32 `json:"uid,omitempty"`
	GID        *uint32 `json:"gid,omitempty"`
}

type InputPackageArchive struct {
	Image     string `yaml:"image"`
	Directory string `yaml:"directory"`
	Volume    string `yaml:"volume"`
}

type InputPackageGitSource struct {
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
	Volume string `yaml:"volume"`
}

// InputPackageTemplate defines a file template in the package manifest.
// Each template renders a Go text/template into a file within a volume.
type InputPackageTemplate struct {
	Volume  string `yaml:"volume"`  // Target volume name (must reference a defined volume)
	Path    string `yaml:"path"`    // File path within the volume (relative)
	Content string `yaml:"content"` // Go text/template content
}

// PackageTemplate is the compiled form of InputPackageTemplate after
// @variable@ substitution has been applied to the Volume and Path fields.
type PackageTemplate struct {
	Volume  string `json:"volume"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

const ImageTypeOCI = "oci"

// InputPackageImage supports both a plain string ("nginx:latest") and a
// structured form ({type: oci, url: nginx:latest}) in YAML.
type InputPackageImage struct {
	Type string `yaml:"type" json:"type,omitempty"`
	URL  string `yaml:"url" json:"url,omitempty"`
}

func (i *InputPackageImage) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		i.Type = ImageTypeOCI
		i.URL = node.Value
		return nil
	}

	// Decode as struct (avoiding infinite recursion via alias).
	type raw InputPackageImage
	var r raw
	if err := node.Decode(&r); err != nil {
		return err
	}
	*i = InputPackageImage(r)
	if i.Type == "" {
		i.Type = ImageTypeOCI
	}
	return nil
}

// InputPackageProton describes how to extract and run a Windows application
// via Valve's Proton compatibility layer.
type InputPackageProton struct {
	AppImage     string   `yaml:"app_image"`
	AppDirectory string   `yaml:"app_directory"`
	Volume       string   `yaml:"volume"`
	Exe          string   `yaml:"exe"`
	Args         []string `yaml:"args,omitempty"`
}

// PackageProton is the compiled form of InputPackageProton.
type PackageProton struct {
	AppImage     string   `json:"app_image"`
	AppDirectory string   `json:"app_directory"`
	Volume       string   `json:"volume"`
	Exe          string   `json:"exe"`
	Args         []string `json:"args,omitempty"`
}

type PackageNetwork struct {
	External PortMap
	Internal PortMap
	Domains  []string
}

type Package struct {
	Image       string
	ImageType   string
	Command     []string
	Environment map[string]string
	Network     PackageNetwork
	Volumes     map[string]PackageVolume
	Templates   map[string]PackageTemplate
	Notes       map[string]string
	Runtime     RuntimeType
	VM          *PackageVM
	Proton      *PackageProton
}

type InputPackageNetwork struct {
	External map[string]string `yaml:"external"`
	Internal map[string]string `yaml:"internal"`
	Domains  []string          `yaml:"domains,omitempty"`
}

type Question struct {
	Query   string     `json:"query" yaml:"query"`
	Type    OutputType `json:"type,omitempty" yaml:"type,omitempty"`
	Default string     `json:"default,omitempty" yaml:"default,omitempty"`
}

type InputPackage struct {
	Image       InputPackageImage                `yaml:"image"`
	Command     []string                         `yaml:"command"`
	Environment map[string]string                `yaml:"environment"`
	Network     InputPackageNetwork              `yaml:"network"`
	Volumes     map[string]InputPackageVolume    `yaml:"volumes"`
	Questions   map[string]Question              `yaml:"questions"`
	Notes       map[string]Note                  `yaml:"notes" json:"notes,omitempty"`
	Description string                           `yaml:"description" json:"description,omitempty"`
	Supplies    []string                         `yaml:"supplies" json:"supplies,omitempty"`
	Archives    []InputPackageArchive            `yaml:"archives,omitempty"`
	GitSources  []InputPackageGitSource          `yaml:"git_sources,omitempty"`
	Templates   map[string]InputPackageTemplate  `yaml:"templates,omitempty"`
	VM          *InputPackageVM                  `yaml:"vm,omitempty" json:"vm,omitempty"`
	Proton      *InputPackageProton              `yaml:"proton,omitempty"`
}

// RuntimeType returns the runtime type for this package based on which
// fields are set. A package with a non-nil VM field uses RuntimeVM;
// otherwise it uses RuntimeContainer.
func (i *InputPackage) RuntimeType() RuntimeType {
	if i.VM != nil {
		return RuntimeVM
	}
	return RuntimeContainer
}

// CompileNotes applies template substitution to the Notes map using the
// provided responses, validates typed notes, and returns the compiled result.
func (i *InputPackage) CompileNotes(responses Responses) (map[string]string, error) {
	if len(i.Notes) == 0 {
		return nil, nil //nolint:nilnil // nil notes is the correct zero value when no notes are defined
	}

	compiled := make(map[string]string, len(i.Notes))
	for k, note := range i.Notes {
		v := applyTemplates(note.Value, responses)
		if err := ValidateNote(v, note.Type); err != nil {
			return nil, fmt.Errorf("note %q: %w", k, err)
		}
		compiled[k] = v
	}

	return compiled, nil
}

// CompileNotesWithContext applies built-in context variables to notes before
// compiling them with user responses. This is used when notes need context
// substitution (e.g. @PACKAGE_DNS@) outside of a full Compile call.
func (i *InputPackage) CompileNotesWithContext(responses Responses, ctx CompileContext) (map[string]string, error) {
	if ctx.ExternalHost != "" {
		for name, note := range i.Notes {
			note.Value = applyTemplate(note.Value, "LOCAL_EXTERNAL_HOST", ctx.ExternalHost)
			i.Notes[name] = note
		}
	}
	if ctx.InternalHost != "" {
		for name, note := range i.Notes {
			note.Value = applyTemplate(note.Value, "LOCAL_INTERNAL_HOST", ctx.InternalHost)
			i.Notes[name] = note
		}
	}
	if ctx.PackageDNS != "" {
		for name, note := range i.Notes {
			note.Value = applyTemplate(note.Value, "PACKAGE_DNS", ctx.PackageDNS)
			i.Notes[name] = note
		}
	}
	return i.CompileNotes(responses)
}

func applyTemplate(input string, v string, repl string) string {
	var inside bool
	tv := ""
	out := ""

	for x := range len(input) {
		switch {
		case input[x] == TemplateChar:
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
		case inside:
			tv = string(append([]byte(tv), input[x]))
		default:
			out = string(append([]byte(out), input[x]))
		}
	}

	if inside {
		out += fmt.Sprintf("%c%s", TemplateChar, tv)
	}

	return out
}

// applyTemplates resolves all template variables in a single pass, avoiding
// re-parsing of @ characters introduced by earlier substitutions. Consecutive
// @@ are treated as a literal @ followed by the start of a new template
// variable (e.g. "git@@domain@" → "git@" + template "domain").
func applyTemplates(input string, responses Responses) string {
	var inside bool
	tv := ""
	out := ""

	for x := range len(input) {
		switch {
		case input[x] == TemplateChar:
			if inside {
				if tv == "" {
					// Consecutive @@ — emit a literal @ and stay
					// inside so the next characters form the real
					// variable name (e.g. "git@@domain@" → "git@" + @domain@).
					out += string(TemplateChar)
				} else {
					inside = false

					if repl, ok := responses[tv]; ok {
						out += repl
					} else {
						out += fmt.Sprintf("%c%s%c", TemplateChar, tv, TemplateChar)
					}

					tv = ""
				}
			} else {
				inside = true
			}
		case inside:
			tv = string(append([]byte(tv), input[x]))
		default:
			out = string(append([]byte(out), input[x]))
		}
	}

	if inside {
		out += fmt.Sprintf("%c%s", TemplateChar, tv)
	}

	return out
}

func (i *InputPackage) iterateFields(iv, response string) {
	for k, v := range i.Environment {
		i.Environment[k] = applyTemplate(v, iv, response)
	}

	out := map[string]string{}

	for s, d := range i.Network.External {
		out[applyTemplate(s, iv, response)] = applyTemplate(d, iv, response)
	}

	i.Network.External = out

	out = map[string]string{}

	for s, d := range i.Network.Internal {
		out[applyTemplate(s, iv, response)] = applyTemplate(d, iv, response)
	}

	i.Network.Internal = out

	for idx := range i.Network.Domains {
		i.Network.Domains[idx] = applyTemplate(i.Network.Domains[idx], iv, response)
	}

	for name := range i.Volumes {
		pv := i.Volumes[name]
		pv.Mountpoint = applyTemplate(pv.Mountpoint, iv, response)
		pv.Quota = applyTemplate(pv.Quota, iv, response)
		pv.Archive = applyTemplate(pv.Archive, iv, response)
		pv.Git = applyTemplate(pv.Git, iv, response)
		i.Volumes[name] = pv
	}

	for idx := range i.GitSources {
		i.GitSources[idx].URL = applyTemplate(i.GitSources[idx].URL, iv, response)
		i.GitSources[idx].Branch = applyTemplate(i.GitSources[idx].Branch, iv, response)
	}

	for name := range i.Templates {
		tmpl := i.Templates[name]
		tmpl.Volume = applyTemplate(tmpl.Volume, iv, response)
		tmpl.Path = applyTemplate(tmpl.Path, iv, response)
		i.Templates[name] = tmpl
	}

	if i.VM != nil {
		i.VM.Image = applyTemplate(i.VM.Image, iv, response)
		i.VM.Memory = applyTemplate(i.VM.Memory, iv, response)
	}

	if i.Proton != nil {
		i.Proton.AppImage = applyTemplate(i.Proton.AppImage, iv, response)
		i.Proton.AppDirectory = applyTemplate(i.Proton.AppDirectory, iv, response)
		i.Proton.Volume = applyTemplate(i.Proton.Volume, iv, response)
		i.Proton.Exe = applyTemplate(i.Proton.Exe, iv, response)
		for idx := range i.Proton.Args {
			i.Proton.Args[idx] = applyTemplate(i.Proton.Args[idx], iv, response)
		}
	}

	for name, note := range i.Notes {
		note.Value = applyTemplate(note.Value, iv, response)
		i.Notes[name] = note
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

	if u == 0 || u > 65535 {
		return 0, ErrInvalidPort
	}

	return uint16(u), nil
}

// Validate checks that all field names and values in the InputPackage conform
// to the expected conventions. It is called automatically by Compile before
// template substitution so that the raw spec (including template markers) is
// validated.
func (i *InputPackage) Validate() error {
	if err := ValidateImageType(i.Image.Type); err != nil {
		return err
	}

	// Exactly one of image, vm, or proton must provide a runtime.
	hasImage := i.Image.URL != ""
	hasVM := i.VM != nil

	// VM is mutually exclusive with image and proton.
	if hasVM && (hasImage || i.Proton != nil) {
		return ErrMixedRuntime
	}
	if !hasImage && !hasVM && i.Proton == nil {
		return ErrNoRuntime
	}

	if hasImage {
		if err := ValidateImageURL(i.Image.URL); err != nil {
			return err
		}
	}

	if hasVM {
		if err := ValidateVMConfig(i.VM); err != nil {
			return err
		}
	}

	// Reject having both command and proton set simultaneously.
	if i.Proton != nil && len(i.Command) > 0 {
		return fmt.Errorf("%w: cannot specify both command and proton", ErrInvalidProtonSpec)
	}

	for key := range i.Environment {
		if err := ValidateEnvironmentKey(key); err != nil {
			return err
		}
	}

	for name := range i.Questions {
		if err := ValidateQuestionName(name); err != nil {
			return err
		}
	}

	for name, vol := range i.Volumes {
		if err := ValidateVolumeName(name); err != nil {
			return err
		}
		// Mountpoints may contain template variables (e.g. "/mnt/@path@"),
		// so only validate literal mountpoints here.
		if !strings.ContainsRune(vol.Mountpoint, TemplateChar) {
			if err := ValidateMountpoint(vol.Mountpoint); err != nil {
				return err
			}
		}
	}

	for name, tmpl := range i.Templates {
		if err := ValidateTemplateName(name); err != nil {
			return err
		}
		if err := ValidateTemplateSpec(tmpl, i.Volumes); err != nil {
			return fmt.Errorf("template %q: %w", name, err)
		}
	}

	return nil
}

// CompileContext provides built-in template variables that are resolved
// during compilation, independent of user responses.
type CompileContext struct {
	ExternalHost string // Replaces @LOCAL_EXTERNAL_HOST@
	InternalHost string // Replaces @LOCAL_INTERNAL_HOST@
	PackageDNS   string // Replaces @PACKAGE_DNS@ — the root DNS name (package.repo.tld)
}

// CompileWithContext compiles the package with both user responses and
// built-in template variables from the provided context.
func (i *InputPackage) CompileWithContext(response Responses, ctx CompileContext) (*Package, error) {
	// Apply built-in template variables before user responses.
	if ctx.ExternalHost != "" {
		i.iterateFields("LOCAL_EXTERNAL_HOST", ctx.ExternalHost)
	}
	if ctx.InternalHost != "" {
		i.iterateFields("LOCAL_INTERNAL_HOST", ctx.InternalHost)
	}
	if ctx.PackageDNS != "" {
		i.iterateFields("PACKAGE_DNS", ctx.PackageDNS)
	}
	return i.Compile(response)
}

func (i *InputPackage) Compile(response Responses) (*Package, error) {
	err := i.Validate()
	if err != nil {
		return nil, err
	}

	for idx, archive := range i.Archives {
		if err := ValidateArchiveSpec(archive, i.Volumes); err != nil {
			return nil, fmt.Errorf("archives[%d]: %w", idx, err)
		}
	}

	for idx, gs := range i.GitSources {
		if err := ValidateGitSource(gs, i.Volumes); err != nil {
			return nil, fmt.Errorf("git_sources[%d]: %w", idx, err)
		}
	}

	if i.Proton != nil {
		if err := ValidateProtonSpec(*i.Proton, i.Volumes); err != nil {
			return nil, fmt.Errorf("proton: %w", err)
		}
	}

	var verrs []ResponseValidationError

	// Check for unknown response keys (question does not exist).
	for prompt := range response {
		if _, ok := i.Questions[prompt]; !ok {
			verrs = append(verrs, ResponseValidationError{
				Name:  prompt,
				Error: ErrInvalidResponse.Error(),
			})
		}
	}

	// Check for missing or empty responses.
	for name := range i.Questions {
		resp, ok := response[name]
		if !ok {
			verrs = append(verrs, ResponseValidationError{
				Name:  name,
				Error: ErrMissingResponse.Error(),
			})
		} else if resp == "" {
			verrs = append(verrs, ResponseValidationError{
				Name:  name,
				Error: ErrEmptyResponse.Error(),
			})
		}
	}

	if len(verrs) > 0 {
		return nil, &ValidationError{Errors: verrs}
	}

	// All responses are present and non-empty; apply templates.
	for prompt, resp := range response {
		q := i.Questions[prompt]
		if q.Type != "" {
			resp, err = q.Type.Output(resp)
			if err != nil {
				verrs = append(verrs, ResponseValidationError{
					Name:  prompt,
					Error: ErrInvalidResponseType.Error(),
				})
				continue
			}
		}
		i.iterateFields(prompt, resp)
	}

	if len(verrs) > 0 {
		return nil, &ValidationError{Errors: verrs}
	}

	external, err := convert(i.Network.External)
	if err != nil {
		return nil, err
	}

	internal, err := convert(i.Network.Internal)
	if err != nil {
		return nil, err
	}

	// Normalize the container image URL after template substitution (container only).
	image := i.Image.URL
	rt := i.RuntimeType()
	if rt == RuntimeContainer {
		image = NormalizeImageURL(image)
	}

	// Normalize image type: empty defaults to OCI.
	imageType := i.Image.Type
	if imageType == "" {
		imageType = ImageTypeOCI
	}

	// Validate mountpoints and parse quotas after template substitution.
	volumes := map[string]PackageVolume{}
	for name, vol := range i.Volumes {
		if err := ValidateMountpoint(vol.Mountpoint); err != nil {
			return nil, fmt.Errorf("volume %q: %w", name, err)
		}

		var quota uint64
		if vol.Quota != "" {
			quota, err = ParseBytes(vol.Quota)
			if err != nil {
				return nil, fmt.Errorf("volume %q quota: %w", name, err)
			}
		}

		if vol.Git != "" {
			if err := ValidateGitURL(vol.Git); err != nil {
				return nil, fmt.Errorf("volume %q: %w", name, err)
			}
		}

		volumes[name] = PackageVolume{Mountpoint: vol.Mountpoint, Quota: quota, Archive: vol.Archive, Git: vol.Git, UID: vol.UID, GID: vol.GID}
	}

	// Compile templates: validate volume references, paths, and content syntax.
	var templates map[string]PackageTemplate
	if len(i.Templates) > 0 {
		templates = make(map[string]PackageTemplate, len(i.Templates))
		for name, tmpl := range i.Templates {
			if _, ok := volumes[tmpl.Volume]; !ok {
				return nil, fmt.Errorf("template %q: volume %q not found in package volumes", name, tmpl.Volume)
			}
			if err := ValidateTemplatePath(tmpl.Path); err != nil {
				return nil, fmt.Errorf("template %q: %w", name, err)
			}
			if _, err := template.New(name).Parse(tmpl.Content); err != nil {
				return nil, fmt.Errorf("template %q: invalid content: %w", name, err)
			}
			templates[name] = PackageTemplate(tmpl)
		}
	}

	command := i.Command
	var proton *PackageProton

	if i.Proton != nil {
		// Auto-generate command: ["proton", "run", exe, ...args]
		command = append([]string{"proton", "run", i.Proton.Exe}, i.Proton.Args...)
		appImage := NormalizeImageURL(i.Proton.AppImage)
		proton = &PackageProton{
			AppImage:     appImage,
			AppDirectory: i.Proton.AppDirectory,
			Volume:       i.Proton.Volume,
			Exe:          i.Proton.Exe,
			Args:         i.Proton.Args,
		}
	}

	notes, err := i.CompileNotes(Responses{})
	if err != nil {
		return nil, err
	}

	p := &Package{
		Image:       image,
		ImageType:   imageType,
		Command:     command,
		Environment: i.Environment,
		Network:     PackageNetwork{External: external, Internal: internal, Domains: i.Network.Domains},
		Volumes:     volumes,
		Templates:   templates,
		Notes:       notes,
		Runtime:     rt,
		Proton:      proton,
	}

	// Compile VM configuration if present.
	if i.VM != nil {
		vmCfg, err := compileVM(i.VM)
		if err != nil {
			return nil, err
		}
		p.VM = vmCfg
	}

	return p, nil
}

// compileVM parses human-readable values in the VM configuration and returns
// a compiled PackageVM.
func compileVM(vm *InputPackageVM) (*PackageVM, error) {
	memory := vm.Memory
	if memory == "" {
		memory = "1gb"
	}
	memBytes, err := ParseBytes(memory)
	if err != nil {
		return nil, fmt.Errorf("vm memory: %w", err)
	}

	cpus := vm.CPUs
	if cpus <= 0 {
		cpus = 1
	}

	return &PackageVM{
		Image:  vm.Image,
		Memory: memBytes,
		CPUs:   cpus,
	}, nil
}
