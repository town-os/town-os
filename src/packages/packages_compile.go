package packages

import (
	"fmt"
	"strconv"
	"strings"
	"text/template"
)

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

	for key, dep := range i.Dependencies {
		for rk, rv := range dep.Responses {
			dep.Responses[rk] = applyTemplate(rv, iv, response)
		}
		i.Dependencies[key] = dep
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

	for key, dep := range i.Dependencies {
		if err := ValidateDependencyName(key); err != nil {
			return fmt.Errorf("dependency %q: %w", key, err)
		}
		if err := ValidateDependencySpec(dep); err != nil {
			return fmt.Errorf("dependency %q: %w", key, err)
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
					resolved.Responses[rk] = applyTemplates(rv, response)
				}
			}
			compiledDeps[key] = resolved
		}
	}

	p := &Package{
		Image:        image,
		ImageType:    imageType,
		Command:      command,
		Environment:  i.Environment,
		Network:      PackageNetwork{External: external, Internal: internal, Domains: i.Network.Domains},
		Volumes:      volumes,
		Templates:    templates,
		Notes:        notes,
		Runtime:      rt,
		Proton:       proton,
		Dependencies: compiledDeps,
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
