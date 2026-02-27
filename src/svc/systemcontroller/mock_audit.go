package systemcontroller

import (
	"context"

	"gitea.com/town-os/town-os/src/account"
)

// --- Audit ---

func (m *MockClient) ListAuditLog(_ context.Context, opts account.AuditListOptions, token string) (*account.AuditPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListAuditLog", Args: []any{opts, token}})

	if m.ListAuditErr != nil {
		return nil, m.ListAuditErr
	}

	entries := make([]account.AuditEntry, len(m.AuditEntries))
	copy(entries, m.AuditEntries)

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	totalPages := (len(entries) + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	return &account.AuditPage{Entries: entries, TotalPages: totalPages, TotalCount: len(entries)}, nil
}
