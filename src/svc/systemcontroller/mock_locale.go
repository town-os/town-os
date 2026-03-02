package systemcontroller

import (
	"context"

	"gitea.com/town-os/town-os/src/i18n"
)

// --- Locales ---

// ListLocales returns the mock locale response.
func (m *MockClient) ListLocales(_ context.Context) (*LocaleListResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListLocales", Args: nil})

	return &LocaleListResponse{
		Current:         i18n.DefaultLocale,
		Populated:       i18n.PopulatedLocales(),
		CommonLanguages: i18n.CommonLanguages,
		ExtendedLocales: i18n.ExtendedLocales,
	}, nil
}
