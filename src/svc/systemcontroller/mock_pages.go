package systemcontroller

import (
	"context"
	"time"

	"gitea.com/town-os/town-os/src/account"
)

func (m *MockClient) CreatePage(_ context.Context, name, repoURL, branch, domain string) (*account.PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "CreatePage", Args: []any{name, repoURL, branch, domain}})

	if m.CreatePageErr != nil {
		return nil, m.CreatePageErr
	}

	if _, exists := m.Pages[name]; exists {
		return nil, account.ErrDuplicatePageName
	}

	if branch == "" {
		branch = "main"
	}

	now := time.Now()
	page := &account.PageSite{
		Name:      name,
		RepoURL:   repoURL,
		Branch:    branch,
		Domain:    domain,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.Pages[name] = page

	out := *page
	return &out, nil
}

func (m *MockClient) UpdatePage(_ context.Context, name string, fields account.PageSiteUpdate) (*account.PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UpdatePage", Args: []any{name, fields}})

	if m.UpdatePageErr != nil {
		return nil, m.UpdatePageErr
	}

	page, ok := m.Pages[name]
	if !ok {
		return nil, account.ErrPageNotFound
	}

	if fields.RepoURL != nil {
		page.RepoURL = *fields.RepoURL
	}
	if fields.Branch != nil {
		page.Branch = *fields.Branch
	}
	if fields.Domain != nil {
		page.Domain = *fields.Domain
	}
	if fields.Status != nil {
		page.Status = *fields.Status
	}
	page.UpdatedAt = time.Now()

	out := *page
	return &out, nil
}

func (m *MockClient) RemovePage(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemovePage", Args: []any{name}})

	if m.RemovePageErr != nil {
		return m.RemovePageErr
	}

	if _, ok := m.Pages[name]; !ok {
		return account.ErrPageNotFound
	}

	delete(m.Pages, name)
	return nil
}

func (m *MockClient) ListPages(_ context.Context, params ListParams) (*PageResult[account.PageSite], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPages", Args: []any{params}})

	if m.ListPagesErr != nil {
		return nil, m.ListPagesErr
	}

	out := make([]account.PageSite, 0, len(m.Pages))
	for _, page := range m.Pages {
		out = append(out, *page)
	}
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}

func (m *MockClient) RebuildPage(_ context.Context, name string) (*account.PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RebuildPage", Args: []any{name}})

	if m.RebuildPageErr != nil {
		return nil, m.RebuildPageErr
	}

	page, ok := m.Pages[name]
	if !ok {
		return nil, account.ErrPageNotFound
	}

	page.Status = "active"
	page.UpdatedAt = time.Now()

	out := *page
	return &out, nil
}
