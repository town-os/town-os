package systemcontroller

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
)

// --- Packages ---

func (m *MockClient) ListTimezones(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListTimezones", Args: nil})

	return packages.ListTimezones(), nil
}

func (m *MockClient) ListFeaturedPackages(_ context.Context) ([]FeaturedRepoGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListFeaturedPackages", Args: nil})

	if m.ListFeaturedErr != nil {
		return nil, m.ListFeaturedErr
	}

	if m.FeaturedGroups != nil {
		return m.FeaturedGroups, nil
	}

	return []FeaturedRepoGroup{}, nil
}

func (m *MockClient) ListPackages(_ context.Context, params ListParams) (*PageResult[PackageListEntry], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackages", Args: []any{params}})

	if m.ListPkgErr != nil {
		return nil, m.ListPkgErr
	}

	entries := make([]PackageListEntry, 0, len(m.Packages))
	for _, pkg := range m.Packages {
		parts := strings.SplitN(pkg, "/", 2)
		var repo, rest string
		if len(parts) == 2 {
			repo = parts[0]
			rest = parts[1]
		} else {
			rest = parts[0]
		}
		nameVer := strings.SplitN(rest, "@", 2)
		name := nameVer[0]
		version := ""
		if len(nameVer) == 2 {
			version = nameVer[1]
		}

		isInstalled := false
		instVersion := ""
		key := fmt.Sprintf("%s/%s", repo, name)
		for _, inst := range m.Installed {
			instParts := strings.SplitN(inst, "/", 2)
			var instRepo, instRest string
			if len(instParts) == 2 {
				instRepo = instParts[0]
				instRest = instParts[1]
			} else {
				instRest = instParts[0]
			}
			instNameVer := strings.SplitN(instRest, "@", 2)
			instName := instNameVer[0]
			if fmt.Sprintf("%s/%s", instRepo, instName) == key {
				isInstalled = true
				if len(instNameVer) == 2 {
					instVersion = instNameVer[1]
				}
				break
			}
		}

		entries = append(entries, PackageListEntry{
			Repo:             repo,
			Name:             name,
			Version:          version,
			Installed:        isInstalled,
			InstalledVersion: instVersion,
		})
	}

	entries = filterSearch(entries, params.Search)
	result := paginate(entries, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) ListPackagesByRepo(_ context.Context, _ ListParams) ([]packages.RepoPackageGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackagesByRepo", Args: nil})

	return nil, nil
}

func (m *MockClient) ListPackageVersions(_ context.Context, name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPackageVersions", Args: []any{name}})

	if m.ListPkgVersionsErr != nil {
		return nil, m.ListPkgVersionsErr
	}

	// Collect versions from Packages list matching name.
	seen := map[string]bool{}
	for _, pkg := range m.Packages {
		parts := strings.SplitN(pkg, "@", 2)
		if len(parts) == 2 && parts[0] == name {
			seen[parts[1]] = true
		}
	}

	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}

	return out, nil
}

func (m *MockClient) GetPackageQuestions(_ context.Context, name string) (map[string]packages.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetPackageQuestions", Args: []any{name}})

	if m.QuestionsErr != nil {
		return nil, m.QuestionsErr
	}

	questions, ok := m.Questions[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}

	out := make(map[string]packages.Question, len(questions))
	maps.Copy(out, questions)
	return out, nil
}

func (m *MockClient) GetPackageQuestionsByIdentity(_ context.Context, repo, name, version string) (map[string]packages.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetPackageQuestionsByIdentity", Args: []any{repo, name, version}})

	if m.QuestionsIdentityErr != nil {
		return nil, m.QuestionsIdentityErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	questions, ok := m.Questions[key]
	if !ok {
		return nil, fmt.Errorf("package %s not found", key)
	}

	out := make(map[string]packages.Question, len(questions))
	maps.Copy(out, questions)
	return out, nil
}

// --- Children ---

func (m *MockClient) ListChildren(_ context.Context, repo, name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListChildren", Args: []any{repo, name}})

	if m.ListChildrenErr != nil {
		return nil, m.ListChildrenErr
	}

	key := fmt.Sprintf("%s/%s", repo, name)
	if m.ChildrenMap != nil {
		if children, ok := m.ChildrenMap[key]; ok {
			return children, nil
		}
	}
	return []string{}, nil
}

// --- Install ---

func (m *MockClient) InstallPreview(_ context.Context, repo, name, version string) (*InstallPreview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "InstallPreview", Args: []any{repo, name, version}})

	if m.InstallPreviewErr != nil {
		return nil, m.InstallPreviewErr
	}
	if m.InstallPreviewResult != nil {
		return m.InstallPreviewResult, nil
	}
	return &InstallPreview{
		Repo:          repo,
		Name:          name,
		Version:       version,
		Volumes:       []VolumePreview{},
		ExternalPorts: []PortPreview{},
		InternalPorts: []PortPreview{},
	}, nil
}

func (m *MockClient) InstallPackage(_ context.Context, name, version string, responses packages.Responses, reuseVolumes bool, importFromVersion string, skipResponseReuse bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "InstallPackage", Args: []any{name, version, responses, reuseVolumes, importFromVersion, skipResponseReuse}})

	if m.InstallPkgErr != nil {
		return m.InstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	m.Installed = append(m.Installed, key)
	m.StoredResponses[key] = responses
	return nil
}

func (m *MockClient) UninstallPackage(_ context.Context, repo, name, version string, purgeVolumes bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UninstallPackage", Args: []any{repo, name, version, purgeVolumes}})

	if m.UninstallPkgErr != nil {
		return m.UninstallPkgErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	for i, p := range m.Installed {
		if p == key {
			m.Installed = append(m.Installed[:i], m.Installed[i+1:]...)
			delete(m.StoredResponses, key)

			if purgeVolumes {
				prefix := fmt.Sprintf("installed/%s/%s/", repo, name)
				for fsName := range m.Filesystems {
					if len(fsName) >= len(prefix) && fsName[:len(prefix)] == prefix {
						delete(m.Filesystems, fsName)
					}
				}
				delete(m.Filesystems, fmt.Sprintf("installed/%s/%s", repo, name))
			}

			return nil
		}
	}

	return fmt.Errorf("%s: not installed", key)
}

func (m *MockClient) PurgeVolumes(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "PurgeVolumes", Args: []any{repo, name}})

	prefix := fmt.Sprintf("installed/%s/%s/", repo, name)
	for fsName := range m.Filesystems {
		if len(fsName) >= len(prefix) && fsName[:len(prefix)] == prefix {
			delete(m.Filesystems, fsName)
		}
	}
	delete(m.Filesystems, fmt.Sprintf("installed/%s/%s", repo, name))

	return nil
}

func (m *MockClient) ListUninstalledVolumes(_ context.Context, repo, name string) (*UninstalledVolumesResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListUninstalledVolumes", Args: []any{repo, name}})

	return &UninstalledVolumesResponse{}, nil
}

func (m *MockClient) PurgeUninstalledVolumes(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "PurgeUninstalledVolumes", Args: []any{repo, name}})

	return nil
}

func (m *MockClient) DisablePackage(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "DisablePackage", Args: []any{repo, name}})

	if m.DisablePkgErr != nil {
		return m.DisablePkgErr
	}

	m.DisabledPackages[name] = true
	return nil
}

func (m *MockClient) EnablePackage(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "EnablePackage", Args: []any{repo, name}})

	if m.EnablePkgErr != nil {
		return m.EnablePkgErr
	}

	delete(m.DisabledPackages, name)
	return nil
}

func (m *MockClient) ListInstalled(_ context.Context, params ListParams) (*PageResult[string], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListInstalled", Args: []any{params}})

	if m.ListInstalledErr != nil {
		return nil, m.ListInstalledErr
	}

	out := make([]string, len(m.Installed))
	copy(out, m.Installed)
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) GetResponses(_ context.Context, repo, name, version string) (packages.Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetResponses", Args: []any{repo, name, version}})

	if m.GetResponsesErr != nil {
		return nil, m.GetResponsesErr
	}

	key := fmt.Sprintf("%s@%s", name, version)
	resp, ok := m.StoredResponses[key]
	if !ok {
		return nil, fmt.Errorf("%s: not installed", key)
	}

	out := make(packages.Responses, len(resp))
	maps.Copy(out, resp)
	return out, nil
}

func (m *MockClient) GetLastResponses(_ context.Context, repo, name string) (packages.Responses, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetLastResponses", Args: []any{repo, name}})

	return packages.Responses{}, nil
}

func (m *MockClient) ClearLastResponses(_ context.Context, repo, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ClearLastResponses", Args: []any{repo, name}})

	return nil
}

func (m *MockClient) GetInstalledInfo(_ context.Context, repo, name, version string) (*InstalledInfoResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "GetInstalledInfo", Args: []any{repo, name, version}})

	key := fmt.Sprintf("%s@%s", name, version)
	resp, ok := m.StoredResponses[key]
	if !ok {
		return nil, fmt.Errorf("%s: not installed", key)
	}

	return &InstalledInfoResponse{
		Responses: resp,
	}, nil
}
