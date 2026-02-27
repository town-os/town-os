package systemcontroller

import (
	"bytes"
	"context"
	"io"
)

// --- Archive ---

func (m *MockClient) UploadArchive(_ context.Context, subvolume string, archiveReader io.Reader, filename, subpath, stopService string) (*ArchiveUploadResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "UploadArchive", Args: []any{subvolume, filename, subpath, stopService}})

	if m.UploadArchiveErr != nil {
		return nil, m.UploadArchiveErr
	}

	if m.UploadArchiveResult != nil {
		return m.UploadArchiveResult, nil
	}

	return &ArchiveUploadResponse{NeedsRestart: true, Message: "archive unpacked successfully"}, nil
}

func (m *MockClient) DownloadArchive(_ context.Context, subvolume string, paths []string, stopService, format string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "DownloadArchive", Args: []any{subvolume, paths, stopService, format}})

	if m.DownloadArchiveErr != nil {
		return nil, m.DownloadArchiveErr
	}

	data := m.DownloadArchiveData
	if data == nil {
		data = []byte("mock-7z-data")
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}
