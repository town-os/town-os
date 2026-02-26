package systemcontroller

import "testing"

func TestSystemdClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*SystemdClient)(nil)
}

func TestMockClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*MockClient)(nil)
}
