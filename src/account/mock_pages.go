package account

import (
	"sync"
	"time"
)

type MockPagesManager struct {
	mu    sync.Mutex
	pages map[string]*PageSite
	Calls []MockCall

	CreateErr error
	GetErr    error
	UpdateErr error
	RemoveErr error
	ListErr   error
}

func InitMockPagesManager() *MockPagesManager {
	return &MockPagesManager{
		pages: map[string]*PageSite{},
	}
}

func (m *MockPagesManager) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

func (m *MockPagesManager) Create(name, repoURL, branch, domain string) (*PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Create", Args: []any{name, repoURL, branch, domain}})

	if m.CreateErr != nil {
		return nil, m.CreateErr
	}

	if name == "" {
		return nil, ErrPageNameRequired
	}
	if repoURL == "" {
		return nil, ErrPageRepoRequired
	}
	if domain == "" {
		return nil, ErrPageDomainRequired
	}

	if _, exists := m.pages[name]; exists {
		return nil, ErrDuplicatePageName
	}

	if branch == "" {
		branch = "main"
	}

	now := time.Now()
	page := &PageSite{
		Name:      name,
		RepoURL:   repoURL,
		Branch:    branch,
		Domain:    domain,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.pages[name] = page

	out := *page
	return &out, nil
}

func (m *MockPagesManager) Get(name string) (*PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Get", Args: []any{name}})

	if m.GetErr != nil {
		return nil, m.GetErr
	}

	page, ok := m.pages[name]
	if !ok {
		return nil, ErrPageNotFound
	}

	out := *page
	return &out, nil
}

func (m *MockPagesManager) Update(name string, fields PageSiteUpdate) (*PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Update", Args: []any{name, fields}})

	if m.UpdateErr != nil {
		return nil, m.UpdateErr
	}

	page, ok := m.pages[name]
	if !ok {
		return nil, ErrPageNotFound
	}

	if fields.RepoURL != nil {
		if *fields.RepoURL == "" {
			return nil, ErrPageRepoRequired
		}
		page.RepoURL = *fields.RepoURL
	}
	if fields.Branch != nil {
		page.Branch = *fields.Branch
	}
	if fields.Domain != nil {
		if *fields.Domain == "" {
			return nil, ErrPageDomainRequired
		}
		page.Domain = *fields.Domain
	}
	if fields.Status != nil {
		page.Status = *fields.Status
	}
	page.UpdatedAt = time.Now()

	out := *page
	return &out, nil
}

func (m *MockPagesManager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Remove", Args: []any{name}})

	if m.RemoveErr != nil {
		return m.RemoveErr
	}

	if _, ok := m.pages[name]; !ok {
		return ErrPageNotFound
	}

	delete(m.pages, name)
	return nil
}

func (m *MockPagesManager) List() ([]PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "List", Args: nil})

	if m.ListErr != nil {
		return nil, m.ListErr
	}

	out := make([]PageSite, 0, len(m.pages))
	for _, page := range m.pages {
		out = append(out, *page)
	}
	return out, nil
}
