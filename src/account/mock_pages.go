package account

import (
	"context"
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

func (m *MockPagesManager) Create(_ context.Context, name, repoURL, branch, domain, sourceType, image, imageDirectory, network string) (*PageSite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Create", Args: []any{name, repoURL, branch, domain, sourceType, image, imageDirectory, network}})

	if m.CreateErr != nil {
		return nil, m.CreateErr
	}

	if name == "" {
		return nil, ErrPageNameRequired
	}
	if domain == "" {
		return nil, ErrPageDomainRequired
	}

	if sourceType == "" {
		sourceType = PageSourceArchive
	}
	if !ValidPageSourceType(sourceType) {
		return nil, ErrPageInvalidSourceType
	}

	switch sourceType {
	case PageSourceGit:
		if repoURL == "" {
			return nil, ErrPageRepoRequired
		}
	case PageSourceContainerImage:
		if image == "" {
			return nil, ErrPageImageRequired
		}
		if imageDirectory == "" {
			return nil, ErrPageImageDirectoryRequired
		}
	}

	if _, exists := m.pages[name]; exists {
		return nil, ErrDuplicatePageName
	}

	if branch == "" {
		branch = "main"
	}

	now := time.Now()
	page := &PageSite{
		Name:           name,
		RepoURL:        repoURL,
		Branch:         branch,
		Domain:         domain,
		SourceType:     sourceType,
		Image:          image,
		ImageDirectory: imageDirectory,
		Status:         "pending",
		Network:        network,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m.pages[name] = page

	out := *page
	return &out, nil
}

func (m *MockPagesManager) Get(_ context.Context, name string) (*PageSite, error) {
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

func (m *MockPagesManager) Update(_ context.Context, name string, fields PageSiteUpdate) (*PageSite, error) {
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
	if fields.SourceType != nil {
		if !ValidPageSourceType(*fields.SourceType) {
			return nil, ErrPageInvalidSourceType
		}
		page.SourceType = *fields.SourceType
	}
	if fields.Image != nil {
		page.Image = *fields.Image
	}
	if fields.ImageDirectory != nil {
		page.ImageDirectory = *fields.ImageDirectory
	}
	if fields.Status != nil {
		page.Status = *fields.Status
	}
	if fields.Network != nil {
		page.Network = *fields.Network
	}
	page.UpdatedAt = time.Now()

	out := *page
	return &out, nil
}

func (m *MockPagesManager) Remove(_ context.Context, name string) error {
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

func (m *MockPagesManager) List(_ context.Context) ([]PageSite, error) {
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
