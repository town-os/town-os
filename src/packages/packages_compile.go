package packages

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
	"text/template"
)

func applyTemplate(input string, v string, repl string) string {
	var inside bool
	var tv, out strings.Builder

	for x := range len(input) {
		switch {
		case input[x] == TemplateChar:
			if inside {
				if tv.Len() == 0 {
					// Consecutive @@ — preserve both characters literally.
					// The @@ escape is only resolved by ApplyTemplates
					// (the single-pass multi-key resolver). The per-key
					// applyTemplate must not modify @@ because it runs
					// multiple passes and would corrupt escapes.
					out.WriteByte(TemplateChar)
					out.WriteByte(TemplateChar)
					inside = false
				} else {
					inside = false

					if tv.String() == v {
						out.WriteString(repl)
					} else {
						out.WriteByte(TemplateChar)
						out.WriteString(tv.String())
						out.WriteByte(TemplateChar)
					}

					tv.Reset()
				}
			} else {
				inside = true
			}
		case inside:
			tv.WriteByte(input[x])
		default:
			out.WriteByte(input[x])
		}
	}

	if inside {
		out.WriteByte(TemplateChar)
		out.WriteString(tv.String())
	}

	return out.String()
}

// ApplyTemplates resolves all template variables in a single pass, avoiding
// re-parsing of @ characters introduced by earlier substitutions. Consecutive
// @@ is a literal @ escape (e.g. "user@@host" → "user@host"). To produce
// a literal @ followed by a template variable, use three @'s:
// "ssh://git@@@PACKAGE_DNS@" → "ssh://git@" + template "PACKAGE_DNS".
func ApplyTemplates(input string, responses Responses) string {
	var inside bool
	var tv, out strings.Builder

	for x := range len(input) {
		switch {
		case input[x] == TemplateChar:
			if inside {
				if tv.Len() == 0 {
					// Consecutive @@ — emit a literal @ and exit
					// inside mode. A subsequent @ starts a fresh
					// template variable (e.g. "@@@var@" → "@" + @var@).
					out.WriteByte(TemplateChar)
					inside = false
				} else {
					inside = false

					tvStr := tv.String()
					if repl, ok := responses[tvStr]; ok {
						out.WriteString(repl)
					} else {
						out.WriteByte(TemplateChar)
						out.WriteString(tvStr)
						out.WriteByte(TemplateChar)
					}

					tv.Reset()
				}
			} else {
				inside = true
			}
		case inside:
			tv.WriteByte(input[x])
		default:
			out.WriteByte(input[x])
		}
	}

	if inside {
		out.WriteByte(TemplateChar)
		out.WriteString(tv.String())
	}

	return out.String()
}

func (i *InputPackage) iterateFields(iv, response string) {
	for k, v := range i.Environment {
		i.Environment[k] = applyTemplate(v, iv, response)
	}

	// Command and Entrypoint args are also user-facing template positions.
	// Without these substitutions a yaml like
	// `command: ["redis-server", "--port", "@port@"]` writes a literal
	// `@port@` into the systemd unit and the container fails on startup.
	for idx := range i.Command {
		i.Command[idx] = applyTemplate(i.Command[idx], iv, response)
	}
	for idx := range i.Entrypoint {
		i.Entrypoint[idx] = applyTemplate(i.Entrypoint[idx], iv, response)
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

	// direct/tls_mode reference the same port keys as external/internal, so
	// they must see the same substitution (e.g. direct: ["@sshport@"]).
	for idx := range i.Network.Direct {
		i.Network.Direct[idx] = applyTemplate(i.Network.Direct[idx], iv, response)
	}
	if len(i.Network.TLSMode) > 0 {
		tlsOut := make(map[string]string, len(i.Network.TLSMode))
		for k, v := range i.Network.TLSMode {
			tlsOut[applyTemplate(k, iv, response)] = applyTemplate(v, iv, response)
		}
		i.Network.TLSMode = tlsOut
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
		tmpl.Content = applyTemplate(tmpl.Content, iv, response)
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

	// Notes are NOT processed here — they are compiled separately via
	// CompileNotes which uses ApplyTemplates (single-pass, handles @@ escape).

	for key, dep := range i.Dependencies {
		for rk, rv := range dep.Responses {
			dep.Responses[rk] = applyTemplate(rv, iv, response)
		}
		// Apply substitution to expose/consume mount paths so they can
		// reference question responses (e.g. path: "/data/@dirname@").
		// The map keys (volume names) and consume.From / consume.Volume
		// are identifiers, not data, and are not substituted.
		for volName, exp := range dep.Expose {
			exp.Path = applyTemplate(exp.Path, iv, response)
			dep.Expose[volName] = exp
		}
		for idx := range dep.Consume {
			dep.Consume[idx].Path = applyTemplate(dep.Consume[idx].Path, iv, response)
		}
		i.Dependencies[key] = dep
	}

	for idx := range i.PostUpdate {
		i.PostUpdate[idx] = applyTemplate(i.PostUpdate[idx], iv, response)
	}
}

// convert translates the YAML network map (host → container port strings)
// into a PortMap and a parallel PortNameMap that records any semantic
// names declared via non-numeric keys.
//
// The YAML key may be either:
//
//   - A numeric port string (legacy form): "5432" → host port 5432,
//     container port = value. No name is recorded.
//   - A semantic name matching PortNameRegexp (named form):
//     e.g. "sql" → the container port (value) is used as the host port
//     too, and the name is recorded in the returned PortNameMap keyed by
//     container port. Parent packages can then reference the dep's port
//     by name via `@dep_<KEY>_port_<NAME>@` in addition to the numeric
//     `@dep_<KEY>_port_<N>@` form.
//
// An empty YAML map yields an empty PortMap and nil PortNameMap.
//
// The returned keyToHost map records the host port each original YAML key
// resolved to, so callers (network.direct / network.tls_mode) can map a
// port key back onto its compiled host port.
func convert(p map[string]string) (PortMap, PortNameMap, map[string]uint16, error) {
	pm := PortMap{}
	var names PortNameMap
	keyToHost := make(map[string]uint16, len(p))

	for key, value := range p {
		containerPort, err := strToPort(value)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("port value %q: %w", value, err)
		}

		hostPort, perr := strToPort(key)
		if perr != nil {
			// Key is not a numeric port; try to interpret as a semantic
			// name. Names and numeric keys may coexist in the same map.
			if !PortNameRegexp.MatchString(key) {
				return nil, nil, nil, fmt.Errorf("%w: %q", ErrInvalidPortName, key)
			}
			if names == nil {
				names = PortNameMap{}
			}
			if prev, dup := names[containerPort]; dup {
				return nil, nil, nil, fmt.Errorf("%w: container port %d has both names %q and %q", ErrInvalidPortName, containerPort, prev, key)
			}
			names[containerPort] = key
			hostPort = containerPort
		}

		pm[hostPort] = containerPort
		keyToHost[key] = hostPort
	}

	return pm, names, keyToHost, nil
}

// compileNetworkPortFlags resolves the network.direct and network.tls_mode
// declarations (keyed by the same port keys as external/internal) onto
// compiled host ports. It validates that every referenced key exists, that
// tls_mode values are recognized, and that a direct port does not also carry
// a tls_mode. Returns nil maps when nothing is declared.
func compileNetworkPortFlags(n InputPackageNetwork, externalKeys, internalKeys map[string]uint16) (map[uint16]bool, map[uint16]TLSMode, error) {
	resolve := func(key string) (uint16, bool) {
		if hp, ok := externalKeys[key]; ok {
			return hp, true
		}
		if hp, ok := internalKeys[key]; ok {
			return hp, true
		}
		return 0, false
	}

	var directPorts map[uint16]bool
	for _, key := range n.Direct {
		hp, ok := resolve(key)
		if !ok {
			return nil, nil, fmt.Errorf("%w: direct %q", ErrUnknownNetworkPortRef, key)
		}
		if directPorts == nil {
			directPorts = map[uint16]bool{}
		}
		directPorts[hp] = true
	}

	var tlsModes map[uint16]TLSMode
	for key, mode := range n.TLSMode {
		hp, ok := resolve(key)
		if !ok {
			return nil, nil, fmt.Errorf("%w: tls_mode %q", ErrUnknownNetworkPortRef, key)
		}
		switch TLSMode(mode) {
		case "", TLSModeTerminate:
			// Default handling — nothing to record (keeps the map sparse).
		case TLSModePassthrough:
			if tlsModes == nil {
				tlsModes = map[uint16]TLSMode{}
			}
			tlsModes[hp] = TLSModePassthrough
		default:
			return nil, nil, fmt.Errorf("%w: %q", ErrInvalidTLSMode, mode)
		}
	}

	// A direct port is opaque TCP owned by the service container, so it can
	// never also be a proxied TLS port. Compare by resolved host port so a
	// numeric `direct` key and a named `tls_mode` key for the same port
	// still clash.
	for key := range n.TLSMode {
		if hp, ok := resolve(key); ok && directPorts[hp] {
			return nil, nil, fmt.Errorf("%w: %q", ErrDirectPortTLSMode, key)
		}
	}

	return directPorts, tlsModes, nil
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
	// Reject packages that declare a proton block in a build without the
	// `proton` tag. When proton is enabled this is a no-op.
	if err := checkProtonAllowed(i); err != nil {
		return err
	}

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

	// Entrypoint overrides the image's ENTRYPOINT at podman-run time and
	// only makes sense for the container runtime. Proton auto-generates a
	// `proton run ...` command (plus a known entrypoint); VM packages are
	// launched via qemu-system-x86_64 with no container concept at all.
	if len(i.Entrypoint) > 0 {
		if hasVM {
			return ErrEntrypointVMNotSupported
		}
		if i.Proton != nil {
			return fmt.Errorf("%w: cannot specify both entrypoint and proton", ErrInvalidProtonSpec)
		}
	}

	for key, value := range i.Environment {
		if err := ValidateEnvironmentKey(key); err != nil {
			return err
		}
		// The author's literal. A marker still to be substituted carries no
		// control character of its own, so this catches a package that ships
		// one and leaves a response that smuggles one to the post-substitution
		// sweep at the end of Compile.
		if err := ValidateNoControlChars("environment "+key, value); err != nil {
			return err
		}
	}

	for idx, arg := range i.Command {
		if err := ValidateNoControlChars(fmt.Sprintf("command[%d]", idx), arg); err != nil {
			return err
		}
	}
	for idx, arg := range i.Entrypoint {
		if err := ValidateNoControlChars(fmt.Sprintf("entrypoint[%d]", idx), arg); err != nil {
			return err
		}
	}

	for name, q := range i.Questions {
		if err := ValidateQuestionName(name); err != nil {
			return err
		}
		// The shape of the flow, not whether its URLs may be dialed: by the time a
		// package is installed the flow has already run, and the address rules
		// belong to the host that ran it (see ServerConfig.OAuthAllowPrivate), not
		// to the package sitting in the repository.
		if err := ValidateOAuthSpec(name, q); err != nil {
			return err
		}
		if err := ValidateShowIf(name, q, i.Questions); err != nil {
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

	siblings := make(map[string]bool, len(i.Dependencies))
	for key := range i.Dependencies {
		siblings[key] = true
	}
	for key, dep := range i.Dependencies {
		if err := ValidateDependencyName(key); err != nil {
			return fmt.Errorf("dependency %q: %w", key, err)
		}
		if err := ValidateDependencySpec(dep); err != nil {
			return fmt.Errorf("dependency %q: %w", key, err)
		}
		for volName, exp := range dep.Expose {
			if err := ValidateDependencyExpose(volName, exp); err != nil {
				return fmt.Errorf("dependency %q expose %q: %w", key, volName, err)
			}
		}
		seenConsumePaths := map[string]bool{}
		for idx, cons := range dep.Consume {
			if err := ValidateDependencyConsume(key, cons, siblings); err != nil {
				return fmt.Errorf("dependency %q consume[%d]: %w", key, idx, err)
			}
			if seenConsumePaths[cons.Path] {
				return fmt.Errorf("dependency %q consume[%d]: %w: duplicate path %q", key, idx, ErrInvalidSharedMount, cons.Path)
			}
			seenConsumePaths[cons.Path] = true
		}
	}

	if hasVM && len(i.PostUpdate) > 0 {
		return ErrPostUpdateVMNotSupported
	}
	for idx, cmd := range i.PostUpdate {
		if strings.TrimSpace(cmd) == "" {
			return fmt.Errorf("post_update[%d]: %w", idx, ErrEmptyPostUpdateCommand)
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

	pkg, err := i.Compile(response)
	if err != nil {
		return nil, err
	}

	// Re-compile notes with context variables merged into responses.
	// Notes are excluded from iterateFields (which uses the per-key
	// applyTemplate and corrupts @@ escapes across passes). Instead,
	// CompileNotesWithContext resolves everything in a single
	// ApplyTemplates pass.
	if len(i.Notes) > 0 {
		notes, notesErr := i.CompileNotesWithContext(response, ctx)
		if notesErr != nil {
			return nil, notesErr
		}
		pkg.Notes = notes
	}

	return pkg, nil
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

	if err := validateProtonCompile(i); err != nil {
		return nil, err
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

	// Check for missing or empty responses. An optional question is exempt from
	// both: it may be absent from the map or answered with an empty string, and
	// it compiles to the empty string either way.
	for name, q := range i.Questions {
		// A hidden conditional question cannot be answered -- its field is not on
		// screen -- so it is exempt from the required check exactly like an
		// optional one, and compiles to empty below.
		if q.Optional || questionHidden(q, i.Questions, response) {
			continue
		}
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

	// Every required response is present and non-empty; apply templates.
	for prompt, resp := range response {
		q := i.Questions[prompt]
		// A hidden conditional question compiles to the empty string no matter
		// what the still-mounted field submitted: the feature it configures is
		// switched off. Force empty and skip Output() so a stale value cannot fail
		// type validation for a field the operator cannot even see.
		if questionHidden(q, i.Questions, response) {
			i.iterateFields(prompt, "")
			continue
		}
		// A blank answer to an optional question substitutes the empty string.
		// It must skip Output(), which exists to reject exactly this for a typed
		// question -- an empty port is not a port.
		if q.Optional && resp == "" {
			i.iterateFields(prompt, "")
			continue
		}
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

	// An optional question the caller omitted entirely still has @marker@ sites
	// to fill. The loop above only walks the responses that were supplied, so
	// without this the marker would survive verbatim into the container's
	// environment -- the app would read a literal "@smtp_host@".
	for name, q := range i.Questions {
		// Same reasoning as the optional case: a hidden conditional question the
		// caller omitted still has @marker@ sites to fill with the empty string,
		// or the app would read a literal "@smtp_host@".
		if !q.Optional && !questionHidden(q, i.Questions, response) {
			continue
		}
		if _, ok := response[name]; !ok {
			i.iterateFields(name, "")
		}
	}

	if len(verrs) > 0 {
		return nil, &ValidationError{Errors: verrs}
	}

	external, externalNames, externalKeys, err := convert(i.Network.External)
	if err != nil {
		return nil, err
	}

	internal, internalNames, internalKeys, err := convert(i.Network.Internal)
	if err != nil {
		return nil, err
	}

	// Resolve network.direct / network.tls_mode (keyed by the same port keys
	// used in external/internal) onto compiled host ports.
	directPorts, tlsModes, err := compileNetworkPortFlags(i.Network, externalKeys, internalKeys)
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

		volumes[name] = PackageVolume{Mountpoint: vol.Mountpoint, Quota: quota, Archive: vol.Archive, Git: vol.Git, UID: vol.UID, GID: vol.GID, Shareable: vol.Shareable}
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

	command, proton := compileProton(i, i.Command)

	notes, err := i.CompileNotes(response)
	if err != nil {
		return nil, err
	}

	// Apply template substitution to dependency response values.
	var compiledDeps map[string]InputPackageDependency
	if len(i.Dependencies) > 0 {
		compiledDeps = make(map[string]InputPackageDependency, len(i.Dependencies))
		for key, dep := range i.Dependencies {
			resolved := InputPackageDependency{
				Package: dep.Package,
				Repo:    dep.Repo,
				Version: dep.Version,
			}
			if len(dep.Responses) > 0 {
				resolved.Responses = make(map[string]string, len(dep.Responses))
				for rk, rv := range dep.Responses {
					resolved.Responses[rk] = ApplyTemplates(rv, response)
				}
			}
			// Carry Expose/Consume forward into the compiled package.
			// Paths were already substituted in iterateFields. Copy the
			// maps/slices so callers cannot mutate the input package.
			if len(dep.Expose) > 0 {
				resolved.Expose = make(map[string]InputDepExpose, len(dep.Expose))
				maps.Copy(resolved.Expose, dep.Expose)
			}
			if len(dep.Consume) > 0 {
				resolved.Consume = make([]InputDepConsume, len(dep.Consume))
				copy(resolved.Consume, dep.Consume)
			}
			compiledDeps[key] = resolved
		}
	}

	// Resolve @@ escapes for Command and Entrypoint: these don't pass
	// through the systemcontroller's runtime applyDepTemplates, so the
	// collapse has to happen here.
	//
	// Environment values are deliberately NOT collapsed here. The
	// systemcontroller runs applyDepTemplates (→ ApplyTemplates) on
	// every env value at install/reconcile time, and that pass already
	// collapses `@@` → `@` as part of its walk — doing it again here
	// would consume the literal `@` that a downstream @dep_*@ template
	// needs to resolve next to. Concrete case: `@dbpass@@@dep_db_host@`
	// must survive compile as `<pass>@@@dep_db_host@` so that the runtime
	// ApplyTemplates pass can emit `@` for the `@@` and substitute
	// `<host>` for `@dep_db_host@`, landing on `<pass>@<host>`. A
	// compile-end collapse turns it into `<pass>@dep_db_host@`, at which
	// point the runtime pass treats the whole `@dep_db_host@` as a
	// template on a bare `@` and substitutes with no leading `@` — so
	// the URL becomes `<pass><host>` and connects to nowhere.
	for idx, arg := range i.Command {
		if strings.Contains(arg, "@@") {
			i.Command[idx] = strings.ReplaceAll(arg, "@@", "@")
		}
	}
	for idx, arg := range i.Entrypoint {
		if strings.Contains(arg, "@@") {
			i.Entrypoint[idx] = strings.ReplaceAll(arg, "@@", "@")
		}
	}

	p := &Package{
		Image:        image,
		ImageType:    imageType,
		Entrypoint:   i.Entrypoint,
		Command:      command,
		Environment:  i.Environment,
		Network:      PackageNetwork{External: external, Internal: internal, ExternalNames: externalNames, InternalNames: internalNames, Domains: i.Network.Domains, DirectPorts: directPorts, TLSModes: tlsModes},
		Volumes:      volumes,
		Templates:    templates,
		Notes:        notes,
		Runtime:      rt,
		Proton:       proton,
		Dependencies: compiledDeps,
		PostUpdate:   trimPostUpdate(i.PostUpdate),
	}

	// Compile VM configuration if present.
	if i.VM != nil {
		vmCfg, err := compileVM(i.VM)
		if err != nil {
			return nil, err
		}
		p.VM = vmCfg
	}

	// The load-bearing half of the control-character check, and the reason it
	// is repeated here rather than only in Validate().
	//
	// Validate() runs at the TOP of Compile, before iterateFields substitutes
	// @markers@, so it only ever sees the author's literals. A question
	// response is substituted afterwards — so a value that is a bare `@pass@`
	// in the YAML passes Validate() and can still arrive carrying a newline.
	// The answer to a question with no `type:` is validated by nothing else at
	// all, which makes the response path the one that actually reaches a unit
	// file with attacker-chosen bytes in it.
	//
	// Sweeping the compiled package rather than the input is what makes this
	// exhaustive: whatever the marker was, whoever supplied it, and whichever
	// pass rewrote it, these are the strings unit generation will emit.
	if err := validateCompiledNoControlChars(p); err != nil {
		return nil, err
	}

	return p, nil
}

// validateCompiledNoControlChars rejects a compiled package whose values would
// break out of their directive when written into a systemd unit file. See
// ValidateNoControlChars for why a raw newline is a privilege boundary and not
// a formatting problem.
//
// It covers the fields that reach ExecStart as podman arguments: environment
// values, the command, the entrypoint, and volume mountpoints (which are
// emitted as part of a `-v host:container:opts` token). Post-update commands
// are checked too — they are run through `podman exec … sh -c`, so a newline
// there is a second command rather than a broken unit, which is its own
// problem.
func validateCompiledNoControlChars(p *Package) error {
	for key, value := range p.Environment {
		if err := ValidateNoControlChars("environment "+key, value); err != nil {
			return err
		}
	}
	for idx, arg := range p.Command {
		if err := ValidateNoControlChars(fmt.Sprintf("command[%d]", idx), arg); err != nil {
			return err
		}
	}
	for idx, arg := range p.Entrypoint {
		if err := ValidateNoControlChars(fmt.Sprintf("entrypoint[%d]", idx), arg); err != nil {
			return err
		}
	}
	for name, vol := range p.Volumes {
		if err := ValidateNoControlChars(fmt.Sprintf("volume %q mountpoint", name), vol.Mountpoint); err != nil {
			return err
		}
	}
	for idx, cmd := range p.PostUpdate {
		if err := ValidateNoControlChars(fmt.Sprintf("post_update[%d]", idx), cmd); err != nil {
			return err
		}
	}
	return nil
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

// trimPostUpdate returns a copy of the commands with leading/trailing
// whitespace stripped from each entry. nil input returns nil.
func trimPostUpdate(cmds []string) []string {
	if len(cmds) == 0 {
		return cmds
	}
	out := make([]string, len(cmds))
	for i, cmd := range cmds {
		out[i] = strings.TrimSpace(cmd)
	}
	return out
}
