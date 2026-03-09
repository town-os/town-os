// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import "testing"

func TestSystemdClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*SystemdClient)(nil)
}

func TestMockClientImplementsClientInterface(t *testing.T) {
	var _ Client = (*MockClient)(nil)
}
