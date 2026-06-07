// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"reflect"
	"testing"
)

// compileNetwork is a small helper that compiles a package carrying only a
// network section plus the questions needed to satisfy any templated ports.
func compileNetwork(t *testing.T, n InputPackageNetwork, questions map[string]Question, responses Responses) (*Package, error) {
	t.Helper()
	in := InputPackage{
		Image:     InputPackageImage{URL: "debian:latest"},
		Network:   n,
		Volumes:   map[string]InputPackageVolume{},
		Questions: questions,
	}
	if responses == nil {
		responses = Responses{}
	}
	return in.Compile(responses)
}

func TestCompileNetworkDirectPorts(t *testing.T) {
	t.Run("numeric key", func(t *testing.T) {
		pkg, err := compileNetwork(t, InputPackageNetwork{
			External: map[string]string{"2222": "22", "443": "8080"},
			Direct:   []string{"2222"},
		}, nil, nil)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !reflect.DeepEqual(pkg.Network.DirectPorts, map[uint16]bool{2222: true}) {
			t.Fatalf("DirectPorts = %v, want {2222:true}", pkg.Network.DirectPorts)
		}
	})

	t.Run("named key resolves to host port", func(t *testing.T) {
		// A named internal port uses its container port as the host port, so
		// "ssh" -> 22 means DirectPorts must contain host port 22.
		pkg, err := compileNetwork(t, InputPackageNetwork{
			Internal: map[string]string{"ssh": "22"},
			Direct:   []string{"ssh"},
		}, nil, nil)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !reflect.DeepEqual(pkg.Network.DirectPorts, map[uint16]bool{22: true}) {
			t.Fatalf("DirectPorts = %v, want {22:true}", pkg.Network.DirectPorts)
		}
	})

	t.Run("templated direct key", func(t *testing.T) {
		pkg, err := compileNetwork(t, InputPackageNetwork{
			External: map[string]string{"@sshport@": "22"},
			Direct:   []string{"@sshport@"},
		}, map[string]Question{
			"sshport": {Query: "ssh port", Type: Port},
		}, Responses{"sshport": "2222"})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !reflect.DeepEqual(pkg.Network.DirectPorts, map[uint16]bool{2222: true}) {
			t.Fatalf("DirectPorts = %v, want {2222:true}", pkg.Network.DirectPorts)
		}
	})
}

func TestCompileNetworkTLSModes(t *testing.T) {
	t.Run("passthrough recorded", func(t *testing.T) {
		pkg, err := compileNetwork(t, InputPackageNetwork{
			External: map[string]string{"https": "8080"},
			TLSMode:  map[string]string{"https": "passthrough"},
		}, nil, nil)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if !reflect.DeepEqual(pkg.Network.TLSModes, map[uint16]TLSMode{8080: TLSModePassthrough}) {
			t.Fatalf("TLSModes = %v, want {8080:passthrough}", pkg.Network.TLSModes)
		}
	})

	t.Run("terminate stays sparse", func(t *testing.T) {
		pkg, err := compileNetwork(t, InputPackageNetwork{
			External: map[string]string{"443": "8080"},
			TLSMode:  map[string]string{"443": "terminate"},
		}, nil, nil)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if pkg.Network.TLSModes != nil {
			t.Fatalf("TLSModes = %v, want nil for terminate", pkg.Network.TLSModes)
		}
	})
}

func TestCompileNetworkBackwardCompatible(t *testing.T) {
	pkg, err := compileNetwork(t, InputPackageNetwork{
		External: map[string]string{"80": "8080"},
		Internal: map[string]string{"sql": "5432"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if pkg.Network.DirectPorts != nil {
		t.Fatalf("DirectPorts = %v, want nil", pkg.Network.DirectPorts)
	}
	if pkg.Network.TLSModes != nil {
		t.Fatalf("TLSModes = %v, want nil", pkg.Network.TLSModes)
	}
}

func TestCompileNetworkValidationErrors(t *testing.T) {
	cases := map[string]struct {
		network InputPackageNetwork
		want    error
	}{
		"unknown direct ref": {
			network: InputPackageNetwork{
				External: map[string]string{"80": "8080"},
				Direct:   []string{"9999"},
			},
			want: ErrUnknownNetworkPortRef,
		},
		"unknown tls_mode ref": {
			network: InputPackageNetwork{
				External: map[string]string{"80": "8080"},
				TLSMode:  map[string]string{"9999": "passthrough"},
			},
			want: ErrUnknownNetworkPortRef,
		},
		"invalid tls_mode value": {
			network: InputPackageNetwork{
				External: map[string]string{"443": "8080"},
				TLSMode:  map[string]string{"443": "garbage"},
			},
			want: ErrInvalidTLSMode,
		},
		"direct port with tls_mode": {
			network: InputPackageNetwork{
				External: map[string]string{"443": "8080"},
				Direct:   []string{"443"},
				TLSMode:  map[string]string{"443": "passthrough"},
			},
			want: ErrDirectPortTLSMode,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileNetwork(t, tc.network, nil, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
