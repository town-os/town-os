package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gitea.com/town-os/town-os/src/packages"
)

// --- Packages ---

// ListTimezones returns the list of available IANA timezone names.
func (c *SystemdClient) ListTimezones(ctx context.Context) (_ []string, err error) {
	resp, err := c.getClient(ctx, "packages/timezones")
	if err != nil {
		return nil, fmt.Errorf("%w: ListTimezones: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/timezones")
	}

	var zones []string
	return zones, json.NewDecoder(resp.Body).Decode(&zones)
}

// ListPackages returns a paginated list of available packages across all repos.
func (c *SystemdClient) ListPackages(ctx context.Context, params ListParams) (_ *PageResult[PackageListEntry], err error) {
	resp, err := c.getClient(ctx, "packages"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackages: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages")
	}

	var page PageResult[PackageListEntry]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// ListPackagesByRepo returns packages grouped by their source repository.
func (c *SystemdClient) ListPackagesByRepo(ctx context.Context, params ListParams) (_ []packages.RepoPackageGroup, err error) {
	resp, err := c.getClient(ctx, "packages/by-repo"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackagesByRepo: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/by-repo")
	}

	var groups []packages.RepoPackageGroup
	return groups, json.NewDecoder(resp.Body).Decode(&groups)
}

// ListPackageVersions returns available versions of the named package.
func (c *SystemdClient) ListPackageVersions(ctx context.Context, name string) (_ []string, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Name: name})

	resp, err := c.postJSON(ctx, "packages/versions", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackageVersions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/versions")
	}

	var versions []string
	return versions, json.NewDecoder(resp.Body).Decode(&versions)
}

// GetPackageQuestions returns the configuration questions for a package by name.
func (c *SystemdClient) GetPackageQuestions(ctx context.Context, name string) (_ map[string]packages.Question, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Name: name})

	resp, err := c.postJSON(ctx, "packages/questions", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetPackageQuestions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/questions")
	}

	var questions map[string]packages.Question
	return questions, json.NewDecoder(resp.Body).Decode(&questions)
}

// GetPackageQuestionsByIdentity returns configuration questions for a specific
// package version identified by repo, name, and version.
func (c *SystemdClient) GetPackageQuestionsByIdentity(ctx context.Context, repo, name, version string) (_ map[string]packages.Question, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageIdentityRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/questions/identity", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetPackageQuestionsByIdentity: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/questions/identity")
	}

	var questions map[string]packages.Question
	return questions, json.NewDecoder(resp.Body).Decode(&questions)
}

// --- Install ---

// InstallPackage installs a package at the given version with the provided
// configuration responses.
func (c *SystemdClient) InstallPackage(ctx context.Context, name, version string, responses packages.Responses, reuseVolumes bool, importFromVersion string, skipResponseReuse bool) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, InstallRequest{
		Name:              name,
		Version:           version,
		Responses:         responses,
		ReuseVolumes:      reuseVolumes,
		ImportFromVersion: importFromVersion,
		SkipResponseReuse: skipResponseReuse,
	})

	return c.postClient(ctx, "packages/install", pr)
}

// UninstallPackage removes an installed package. Set purgeVolumes to also
// delete all associated data volumes.
func (c *SystemdClient) UninstallPackage(ctx context.Context, repo, name, version string, purgeVolumes bool) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UninstallRequest{Repo: repo, Name: name, Version: version, PurgeVolumes: purgeVolumes})

	return c.postClient(ctx, "packages/uninstall", pr)
}

// PurgeVolumes deletes all data volumes for the named package.
func (c *SystemdClient) PurgeVolumes(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/purge-volumes", pr)
}

// ListUninstalledVolumes returns information about volumes left behind by
// previously uninstalled packages.
func (c *SystemdClient) ListUninstalledVolumes(ctx context.Context, repo, name string) (_ *UninstalledVolumesResponse, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	resp, err := c.postJSON(ctx, "packages/uninstalled-volumes", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListUninstalledVolumes: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/uninstalled-volumes")
	}

	var result UninstalledVolumesResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// PurgeUninstalledVolumes deletes leftover volumes from uninstalled packages.
func (c *SystemdClient) PurgeUninstalledVolumes(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/purge-uninstalled-volumes", pr)
}

// DisablePackage stops a package's services without uninstalling it.
func (c *SystemdClient) DisablePackage(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageToggleRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/disable", pr)
}

// EnablePackage re-enables a previously disabled package.
func (c *SystemdClient) EnablePackage(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageToggleRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/enable", pr)
}

// ListInstalled returns a paginated list of installed package identifiers.
func (c *SystemdClient) ListInstalled(ctx context.Context, params ListParams) (_ *PageResult[string], err error) {
	resp, err := c.getClient(ctx, "packages/installed"+params.QueryString())
	if err != nil {
		return nil, fmt.Errorf("%w: ListInstalled: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/installed")
	}

	var page PageResult[string]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// GetResponses returns the stored configuration responses for an installed package.
func (c *SystemdClient) GetResponses(ctx context.Context, repo, name, version string) (_ packages.Responses, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetResponsesRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/responses", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetResponses: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/responses")
	}

	var responses packages.Responses
	return responses, json.NewDecoder(resp.Body).Decode(&responses)
}

// GetLastResponses returns the most recently stored configuration responses
// for a package (across all versions).
func (c *SystemdClient) GetLastResponses(ctx context.Context, repo, name string) (_ packages.Responses, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	resp, err := c.postJSON(ctx, "packages/last-responses", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetLastResponses: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/last-responses")
	}

	var responses packages.Responses
	return responses, json.NewDecoder(resp.Body).Decode(&responses)
}

// ClearLastResponses removes the cached last-responses for a package.
func (c *SystemdClient) ClearLastResponses(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/clear-last-responses", pr)
}

// GetInstalledInfo returns detailed information about an installed package.
func (c *SystemdClient) GetInstalledInfo(ctx context.Context, repo, name, version string) (_ *InstalledInfoResponse, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageIdentityRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/installed/info", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetInstalledInfo: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/installed/info")
	}

	var info InstalledInfoResponse
	return &info, json.NewDecoder(resp.Body).Decode(&info)
}

// ListChildren returns the child package names for a given repo and package.
func (c *SystemdClient) ListChildren(ctx context.Context, repo, name string) (_ []string, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, ListChildrenRequest{Repo: repo, Name: name})

	resp, err := c.postJSON(ctx, "packages/children", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListChildren: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/children")
	}

	var children []string
	return children, json.NewDecoder(resp.Body).Decode(&children)
}

// InstallPreview returns volume and port information that would be created
// if the package were installed, without performing the installation.
func (c *SystemdClient) InstallPreview(ctx context.Context, repo, name, version string) (_ *InstallPreview, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, InstallPreviewRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/install-preview", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: InstallPreview: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/install-preview")
	}

	var preview InstallPreview
	return &preview, json.NewDecoder(resp.Body).Decode(&preview)
}
