package packages

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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
)

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

type PackageNetwork struct {
	External PortMap
	Internal PortMap
}

type Package struct {
	Image       string
	Command     []string
	Environment map[string]string
	Network     PackageNetwork
	Volumes     map[string]PackageVolume
}

type InputPackageNetwork struct {
	External map[string]string `yaml:"external"`
	Internal map[string]string `yaml:"internal"`
	// TODO: hostname (with validation)
}

type Question struct {
	Query   string     `json:"query" yaml:"query"`
	Type    OutputType `json:"type,omitempty" yaml:"type,omitempty"`
	Default string     `json:"default,omitempty" yaml:"default,omitempty"`
}

type InputPackage struct {
	Image       string                        `yaml:"image"`
	Command     []string                      `yaml:"command"`
	Environment map[string]string             `yaml:"environment"`
	Network     InputPackageNetwork           `yaml:"network"`
	Volumes     map[string]InputPackageVolume `yaml:"volumes"`
	Questions   map[string]Question           `yaml:"questions"`
	Notes       map[string]Note               `yaml:"notes" json:"notes,omitempty"`
	Description string                        `yaml:"description" json:"description,omitempty"`
	Supplies    []string                      `yaml:"supplies" json:"supplies,omitempty"`
	Archives    []InputPackageArchive         `yaml:"archives,omitempty"`
}

// CompileNotes applies template substitution to the Notes map using the
// provided responses, validates typed notes, and returns the compiled result.
func (i *InputPackage) CompileNotes(responses Responses) (map[string]string, error) {
	if len(i.Notes) == 0 {
		return map[string]string{}, nil
	}

	compiled := make(map[string]string, len(i.Notes))
	for k, note := range i.Notes {
		v := note.Value
		for rk, rv := range responses {
			v = applyTemplate(v, rk, rv)
		}
		if err := ValidateNote(v, note.Type); err != nil {
			return nil, fmt.Errorf("note %q: %w", k, err)
		}
		compiled[k] = v
	}

	return compiled, nil
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

	for name := range i.Volumes {
		pv := i.Volumes[name]
		pv.Mountpoint = applyTemplate(pv.Mountpoint, iv, response)
		pv.Quota = applyTemplate(pv.Quota, iv, response)
		pv.Archive = applyTemplate(pv.Archive, iv, response)
		pv.Git = applyTemplate(pv.Git, iv, response)
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
	err := ValidateImageURL(i.Image)
	if err != nil {
		return err
	}

	for key := range i.Environment {
		err := ValidateEnvironmentKey(key)
		if err != nil {
			return err
		}
	}

	for name := range i.Questions {
		err := ValidateQuestionName(name)
		if err != nil {
			return err
		}
	}

	for name, vol := range i.Volumes {
		err := ValidateVolumeName(name)
		if err != nil {
			return err
		}
		// Mountpoints may contain template variables (e.g. "/mnt/@path@"),
		// so only validate literal mountpoints here.
		if !strings.ContainsRune(vol.Mountpoint, TemplateChar) {
			err := ValidateMountpoint(vol.Mountpoint)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// CompileContext provides built-in template variables that are resolved
// during compilation, independent of user responses.
type CompileContext struct {
	ExternalHost string // Replaces @LOCAL_EXTERNAL_HOST@
	InternalHost string // Replaces @LOCAL_INTERNAL_HOST@
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

	// Normalize the container image URL after template substitution.
	image := NormalizeImageURL(i.Image)

	// Validate mountpoints and parse quotas after template substitution.
	volumes := map[string]PackageVolume{}
	for name, vol := range i.Volumes {
		err := ValidateMountpoint(vol.Mountpoint)
		if err != nil {
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

	p := &Package{
		Image:       image,
		Command:     i.Command,
		Environment: i.Environment,
		Network:     PackageNetwork{External: external, Internal: internal},
		Volumes:     volumes,
	}

	return p, nil
}
