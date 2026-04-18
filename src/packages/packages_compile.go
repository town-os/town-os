package packages

import (
	"fmt"
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
func convert(p map[string]string) (PortMap, PortNameMap, error) {
	pm := PortMap{}
	var names PortNameMap

	for key, value := range p {
		containerPort, err := strToPort(value)
		if err != nil {
			return nil, nil, fmt.Errorf("port value %q: %w", value, err)
		}

		hostPort, perr := strToPort(key)
		if perr != nil {
			// Key is not a numeric port; try to interpret as a semantic
			// name. Names and numeric keys may coexist in the same map.
			if !PortNameRegexp.MatchString(key) {
				return nil, nil, fmt.Errorf("%w: %q", ErrInvalidPortName, key)
			}
			if names == nil {
				names = PortNameMap{}
			}
			if prev, dup := names[containerPort]; dup {
				return nil, nil, fmt.Errorf("%w: container port %d has both names %q and %q", ErrInvalidPortName, containerPort, prev, key)
			}
			names[containerPort] = key
			hostPort = containerPort
		}

		pm[hostPort] = containerPort
	}

	return pm, names, nil
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

	for key, dep := range i.Dependencies {
		if err := ValidateDependencyName(key); err != nil {
			return fmt.Errorf("dependency %q: %w", key, err)
		}
		if err := ValidateDependencySpec(dep); err != nil {
			return fmt.Errorf("dependency %q: %w", key, err)
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

	external, externalNames, err := convert(i.Network.External)
	if err != nil {
		return nil, err
	}

	internal, internalNames, err := convert(i.Network.Internal)
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
			compiledDeps[key] = resolved
		}
	}

	// Resolve @@ escapes in environment values. The per-key applyTemplate
	// preserves @@ across passes to avoid corruption; now that all passes
	// are done, collapse @@ → @ for the final output.
	for k, v := range i.Environment {
		if strings.Contains(v, "@@") {
			i.Environment[k] = strings.ReplaceAll(v, "@@", "@")
		}
	}

	p := &Package{
		Image:        image,
		ImageType:    imageType,
		Entrypoint:   i.Entrypoint,
		Command:      command,
		Environment:  i.Environment,
		Network:      PackageNetwork{External: external, Internal: internal, ExternalNames: externalNames, InternalNames: internalNames, Domains: i.Network.Domains},
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
