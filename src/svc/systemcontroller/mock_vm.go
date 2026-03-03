package systemcontroller

import "context"

// ListVMImages returns the cached VM images from the mock.
// Returns an empty slice when VMImages is nil.
//
// Calls GET /vm-images on the Control Plane Service.
func (m *MockClient) ListVMImages(_ context.Context) ([]VMImageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "ListVMImages"})

	if m.ListVMImagesErr != nil {
		return nil, m.ListVMImagesErr
	}

	if m.VMImages != nil {
		return m.VMImages, nil
	}

	return []VMImageInfo{}, nil
}

// UploadVMImage records an upload call and returns the configured result.
//
// Parameters:
//   - url: remote URL to download the VM image from (required).
//   - name: desired filename for the cached image.
//
// Calls POST /vm-images/upload on the Control Plane Service.
func (m *MockClient) UploadVMImage(_ context.Context, url, name string) (*VMImageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "UploadVMImage", Args: []any{url, name}})

	if m.UploadVMImageErr != nil {
		return nil, m.UploadVMImageErr
	}

	if m.UploadVMImageResult != nil {
		return m.UploadVMImageResult, nil
	}

	return &VMImageInfo{Name: name}, nil
}

// DeleteVMImage records a delete call and returns the configured error.
//
// Parameters:
//   - name: filename of the VM image to delete (required).
//
// Calls POST /vm-images/delete on the Control Plane Service.
func (m *MockClient) DeleteVMImage(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, MockCall{Method: "DeleteVMImage", Args: []any{name}})

	return m.DeleteVMImageErr
}
