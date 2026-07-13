package packages

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strings"

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
	ErrInvalidVMConfig          = errors.New("invalid vm configuration")
	ErrPostUpdateVMNotSupported = errors.New("post_update is not supported for VM packages")
	ErrEntrypointVMNotSupported = errors.New("entrypoint is not supported for VM packages")
	ErrEmptyPostUpdateCommand   = errors.New("post_update command must not be empty")

	// ErrUnknownNetworkPortRef is returned when network.direct or
	// network.tls_mode references a port key that is not declared in
	// network.external or network.internal.
	ErrUnknownNetworkPortRef = errors.New("network port reference does not match any external/internal port")
	// ErrInvalidTLSMode is returned when a network.tls_mode value is not one
	// of the recognized modes ("terminate" or "passthrough").
	ErrInvalidTLSMode = errors.New("invalid tls_mode (must be \"terminate\" or \"passthrough\")")
	// ErrDirectPortTLSMode is returned when a port is listed in
	// network.direct and also carries a network.tls_mode entry. A direct
	// port is opaque TCP host-published by the service container itself, so
	// the network controller never fronts it and TLS handling is the
	// service's own concern.
	ErrDirectPortTLSMode = errors.New("a direct port cannot also declare a tls_mode")
)

// TLSMode selects how the network controller handles TLS for a proxied port.
type TLSMode string

const (
	// TLSModeTerminate (default) terminates TLS at the network controller
	// using the package's local-CA leaf certificate and reverse-proxies
	// plaintext to the backing service over the shared podman network.
	TLSModeTerminate TLSMode = "terminate"
	// TLSModePassthrough routes the connection to the backing service by
	// SNI at layer 4 without decrypting it; the backing service presents
	// its own certificate end to end.
	TLSModePassthrough TLSMode = "passthrough"
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
	// Shareable opts the volume in to being bind-mounted into another
	// package's container in the same dependency tree (parent via
	// `dependencies.<key>.expose:`, sibling via `dependencies.<key>.consume:`).
	// Volumes without this flag cannot be referenced as a shared-mount
	// source — the cross-package install/reconcile pass rejects them.
	Shareable bool `yaml:"shareable,omitempty"`
}

type PackageVolume struct {
	Mountpoint string  `json:"mountpoint"`
	Quota      uint64  `json:"quota,omitempty"`
	Archive    string  `json:"archive,omitempty"`
	Git        string  `json:"git,omitempty"`
	UID        *uint32 `json:"uid,omitempty"`
	GID        *uint32 `json:"gid,omitempty"`
	Shareable  bool    `json:"shareable,omitempty"`
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

// PortNameMap maps a container port number to an optional semantic name
// (e.g. "sql", "http"). Names originate from the YAML keys of
// network.external / network.internal when the key is not itself a port
// number. They are used to emit named TOWNOS_DEP_<KEY>_PORT_<NAME> env
// vars on parent packages so siblings can reference dep ports by role
// (`@dep_db_port_sql@`) instead of by the raw container port number
// (`@dep_db_port_5432@`). A missing entry means the port has no name.
type PortNameMap map[uint16]string

type PackageNetwork struct {
	External      PortMap
	Internal      PortMap
	ExternalNames PortNameMap
	InternalNames PortNameMap
	Domains       []string
	// DirectPorts marks host ports (keys of External/Internal) that the
	// service container must host-publish itself with `-p`. The network
	// controller leaves these ports alone — they are opaque TCP for a
	// service "programmed to operate on a specific port". A nil/absent map
	// means every port is fronted by the network controller (the default).
	DirectPorts map[uint16]bool `json:"direct_ports,omitempty"`
	// TLSModes carries the non-default TLS handling per host port. Only
	// passthrough entries are recorded (terminate is the default and the
	// zero value), so the map is sparse and nil for the common case.
	TLSModes map[uint16]TLSMode `json:"tls_modes,omitempty"`
}

type Package struct {
	Image     string
	ImageType string
	// Entrypoint, when non-empty, replaces the container image's built-in
	// ENTRYPOINT (container runtime only; rejected for VM and Proton).
	// Required for images whose upstream ENTRYPOINT is a wrapper script
	// that rejects arbitrary Command args — e.g. matrixdotorg/synapse's
	// /start.py interprets the first arg as a "mode" and errors on any
	// unknown value, so a package that wants to run synapse via
	// `command: [sh, -c, "..."]` must also set `entrypoint: [sh, -c]` to
	// replace /start.py outright.
	Entrypoint   []string
	Command      []string
	Environment  map[string]string
	Network      PackageNetwork
	Volumes      map[string]PackageVolume
	Templates    map[string]PackageTemplate
	Notes        map[string]string
	Runtime      RuntimeType
	VM           *PackageVM
	Proton       *PackageProton
	Dependencies map[string]InputPackageDependency
	PostUpdate   []string
}

type InputPackageNetwork struct {
	External map[string]string `yaml:"external"`
	Internal map[string]string `yaml:"internal"`
	Domains  []string          `yaml:"domains,omitempty"`
	// Direct lists port keys (the same numeric or semantic keys used in
	// external/internal) whose host binding is published by the service
	// container itself, bypassing the network controller proxy. Use this
	// escape hatch for services programmed to operate on a fixed host port.
	Direct []string `yaml:"direct,omitempty"`
	// TLSMode maps a port key (as used in external/internal) to its TLS
	// handling: "terminate" (default) or "passthrough". Absent keys
	// terminate at the proxy.
	TLSMode map[string]string `yaml:"tls_mode,omitempty"`
}

// OAuthStep is a single HTTP call in a device-flow: the one that starts the
// flow, and the one polled until the user approves it.
type OAuthStep struct {
	Method  string            `json:"method,omitempty" yaml:"method,omitempty"`
	URL     string            `json:"url" yaml:"url"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	// Form is sent as an application/x-www-form-urlencoded body. Device flows
	// split about evenly between form bodies (GitHub) and bare query strings
	// (Plex), so both have to be expressible.
	Form map[string]string `json:"form,omitempty" yaml:"form,omitempty"`
}

// OAuthFlow describes an OAuth 2.0 device flow: create a pending authorization,
// send the user somewhere to approve it, then poll until a token comes back.
// This is the only OAuth variant that works here, because Town OS has no public
// redirect URI to receive an authorization code at.
//
// The URLs live in the package rather than in a provider registry inside Town
// OS, so a package author can wire up a new service without changing the system
// controller. The server makes these calls, so it restricts them to https and
// refuses hosts that resolve to private or loopback addresses -- otherwise a
// package could use this to probe the network the controller runs on.
//
// Templates use {{name}} and resolve against the values pulled out by Start's
// Extract map, plus {{client_id}}, a UUID minted per flow (Plex requires a
// stable client identifier across the create/poll pair).
type OAuthFlow struct {
	Start OAuthStep `json:"start" yaml:"start"`
	// Extract names the JSON fields to pull out of Start's response and make
	// available to the templates below.
	Extract map[string]string `json:"extract,omitempty" yaml:"extract,omitempty"`
	// Approve is the URL the user opens to approve the request.
	Approve string `json:"approve" yaml:"approve"`
	// UserCode is an optional template for a short code the user must type on
	// the approval page (GitHub shows one; Plex does not).
	UserCode string    `json:"user_code,omitempty" yaml:"user_code,omitempty"`
	Poll     OAuthStep `json:"poll" yaml:"poll"`
	// Token is the JSON field in Poll's response holding the token. It stays
	// absent or null until the user approves, which is what "pending" means.
	Token string `json:"token" yaml:"token"`
	// Interval and Timeout are human-readable durations (e.g. "5s", "5m").
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout  string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

type Question struct {
	Query string     `json:"query" yaml:"query"`
	Type  OutputType `json:"type,omitempty" yaml:"type,omitempty"`
	// OAuth is required by, and only meaningful for, an `oauth` question.
	OAuth *OAuthFlow `json:"oauth,omitempty" yaml:"oauth,omitempty"`
	// Optional lets a question be left blank. Every other question must be
	// answered with a non-empty value, which makes settings a package can
	// genuinely do without -- an SMTP relay, an API key -- impossible to
	// express: the author has to invent a placeholder default and hope the
	// operator overwrites it. An optional question accepts an empty answer and
	// substitutes the empty string at its @marker@ sites, so the application
	// sees the variable unset rather than set to something made up.
	Optional bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
	Default  string `json:"default,omitempty" yaml:"default,omitempty"`
}

type InputPackage struct {
	Image        InputPackageImage                         `yaml:"image"`
	Entrypoint   []string                                  `yaml:"entrypoint,omitempty"`
	Command      []string                                  `yaml:"command"`
	Environment  map[string]string                         `yaml:"environment"`
	Network      InputPackageNetwork                       `yaml:"network"`
	Volumes      map[string]InputPackageVolume             `yaml:"volumes"`
	Questions    map[string]Question                       `yaml:"questions"`
	Notes        map[string]Note                           `yaml:"notes" json:"notes,omitempty"`
	Description  string                                    `yaml:"description" json:"description,omitempty"`
	Supplies     []string                                  `yaml:"supplies" json:"supplies,omitempty"`
	Archives     []InputPackageArchive                     `yaml:"archives,omitempty"`
	GitSources   []InputPackageGitSource                   `yaml:"git_sources,omitempty"`
	Templates    map[string]InputPackageTemplate           `yaml:"templates,omitempty"`
	VM           *InputPackageVM                           `yaml:"vm,omitempty" json:"vm,omitempty"`
	Proton       *InputPackageProton                       `yaml:"proton,omitempty"`
	Dependencies map[string]InputPackageDependency         `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	PostUpdate   []string                                  `yaml:"post_update,omitempty" json:"post_update,omitempty"`
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
		v := ApplyTemplates(note.Value, responses)
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
	// Merge context variables and user responses into a single map so
	// ApplyTemplates resolves everything in one pass. This correctly
	// handles @@ escapes (e.g. "ssh://git@@@PACKAGE_DNS@").
	merged := make(Responses, len(responses)+3)
	maps.Copy(merged, responses)
	if ctx.ExternalHost != "" {
		merged["LOCAL_EXTERNAL_HOST"] = ctx.ExternalHost
	}
	if ctx.InternalHost != "" {
		merged["LOCAL_INTERNAL_HOST"] = ctx.InternalHost
	}
	if ctx.PackageDNS != "" {
		merged["PACKAGE_DNS"] = ctx.PackageDNS
	}
	return i.CompileNotes(merged)
}
