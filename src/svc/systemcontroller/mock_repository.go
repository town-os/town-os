package systemcontroller

import (
	"context"
	"fmt"
)

// --- Repository ---

func (m *MockClient) AddRepository(_ context.Context, name, rawURL, username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "AddRepository", Args: []any{name, rawURL, username, password}})

	if m.AddRepoErr != nil {
		return m.AddRepoErr
	}

	for _, r := range m.Repositories {
		if r.URL == rawURL {
			return fmt.Errorf("repository %s already exists", rawURL)
		}
	}

	if name == "" {
		name = rawURL
	}
	m.Repositories = append(m.Repositories, RepositoryInfo{Name: name, URL: rawURL})
	return nil
}

func (m *MockClient) RemoveRepository(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemoveRepository", Args: []any{name}})

	if m.RemRepoErr != nil {
		return m.RemRepoErr
	}

	for i, r := range m.Repositories {
		if r.Name == name {
			m.Repositories = append(m.Repositories[:i], m.Repositories[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("repository %s not found", name)
}

func (m *MockClient) MoveRepository(_ context.Context, name string, position int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "MoveRepository", Args: []any{name, position}})
	return nil
}

func (m *MockClient) RefreshRepositories(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RefreshRepositories", Args: nil})
	return map[string]string{}, nil
}

func (m *MockClient) ListRepositories(_ context.Context, params ListParams) (*PageResult[RepositoryInfo], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListRepositories", Args: []any{params}})

	if m.ListRepoErr != nil {
		return nil, m.ListRepoErr
	}

	out := make([]RepositoryInfo, len(m.Repositories))
	copy(out, m.Repositories)
	out = filterSearch(out, params.Search)
	result := paginate(out, params.Limit, params.Offset)
	return &result, nil
}
