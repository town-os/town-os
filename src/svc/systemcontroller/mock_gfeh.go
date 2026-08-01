// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/http"
	"sort"

	"gitea.com/town-os/town-os/src/storage"
)

// Mock implementations of the four /gfeh/partitions/* contract calls.
//
// They reproduce the status codes gfeh's client branches on — 409 on a
// duplicate, 404 on an absent partition — because a mock that always succeeded
// would hide exactly the case gfehd's create-or-resize provisioning depends on.

func (m *MockClient) gfehPartitionsLocked() map[string]storage.Filesystem {
	if m.GfehPartitions == nil {
		m.GfehPartitions = map[string]storage.Filesystem{}
	}
	return m.GfehPartitions
}

// CreateGfehPartition records a new partition, or reports a conflict.
func (m *MockClient) CreateGfehPartition(_ context.Context, name string, quota uint64) (storage.Filesystem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "CreateGfehPartition", Args: []any{name, quota}})

	if m.CreateGfehPartitionErr != nil {
		return storage.Filesystem{}, m.CreateGfehPartitionErr
	}

	volume := GfehVolumePrefix + "/" + name
	parts := m.gfehPartitionsLocked()
	if _, exists := parts[volume]; exists {
		return storage.Filesystem{}, &ProblemError{
			Method:  "POST",
			Path:    "gfeh/partitions/create",
			Problem: ProblemDetailError{Status: http.StatusConflict, Detail: "partition already exists"},
		}
	}

	fs := storage.Filesystem{Name: volume, Quota: quota}
	parts[volume] = fs
	return fs, nil
}

// ModifyGfehPartition changes a recorded partition's quota.
func (m *MockClient) ModifyGfehPartition(_ context.Context, name string, quota uint64) (storage.Filesystem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "ModifyGfehPartition", Args: []any{name, quota}})

	if m.ModifyGfehPartitionErr != nil {
		return storage.Filesystem{}, m.ModifyGfehPartitionErr
	}

	volume := GfehVolumePrefix + "/" + name
	parts := m.gfehPartitionsLocked()
	if _, exists := parts[volume]; !exists {
		return storage.Filesystem{}, &ProblemError{
			Method:  "POST",
			Path:    "gfeh/partitions/modify",
			Problem: ProblemDetailError{Status: http.StatusNotFound, Detail: "partition not found"},
		}
	}

	fs := storage.Filesystem{Name: volume, Quota: quota}
	parts[volume] = fs
	return fs, nil
}

// RemoveGfehPartition drops a recorded partition.
func (m *MockClient) RemoveGfehPartition(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "RemoveGfehPartition", Args: []any{name}})

	if m.RemoveGfehPartitionErr != nil {
		return m.RemoveGfehPartitionErr
	}

	volume := GfehVolumePrefix + "/" + name
	parts := m.gfehPartitionsLocked()
	if _, exists := parts[volume]; !exists {
		return &ProblemError{
			Method:  "POST",
			Path:    "gfeh/partitions/remove",
			Problem: ProblemDetailError{Status: http.StatusNotFound, Detail: "partition not found"},
		}
	}
	delete(parts, volume)
	return nil
}

// ListGfehPartitions returns every recorded partition, sorted, with names
// carrying the gfeh/ prefix — the shape the real route answers.
func (m *MockClient) ListGfehPartitions(_ context.Context) ([]storage.Filesystem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "ListGfehPartitions"})

	if m.ListGfehPartitionsErr != nil {
		return nil, m.ListGfehPartitionsErr
	}

	parts := m.gfehPartitionsLocked()
	out := make([]storage.Filesystem, 0, len(parts))
	for _, fs := range parts {
		out = append(out, fs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
