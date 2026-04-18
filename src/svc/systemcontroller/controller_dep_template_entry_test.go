// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"reflect"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

// buildTemplateDepEntry is the source of truth that file templates see via
// .Dep.KEY.Host / .Dep.KEY.Ports, so these tests pin down the exact shape.
// Everything else is plumbing; if this struct is wrong, matrix (and any
// future dep-using package) silently renders garbage into its config file.
func TestBuildTemplateDepEntry(t *testing.T) {
	t.Run("numeric-only port surfaces only as numeric key", func(t *testing.T) {
		got := buildTemplateDepEntry("c-db", &packages.Package{
			Network: packages.PackageNetwork{
				External: packages.PortMap{5432: 5432},
			},
		})
		want := packages.TemplateDep{
			Host:  "c-db",
			Ports: map[string]string{"5432": "5432"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("named port surfaces under both numeric and lowercased-name keys", func(t *testing.T) {
		got := buildTemplateDepEntry("c-db", &packages.Package{
			Network: packages.PackageNetwork{
				External:      packages.PortMap{5432: 5432},
				ExternalNames: packages.PortNameMap{5432: "SQL"},
			},
		})
		want := packages.TemplateDep{
			Host:  "c-db",
			Ports: map[string]string{"5432": "5432", "sql": "5432"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("external and internal ports merge into one Ports map", func(t *testing.T) {
		got := buildTemplateDepEntry("c-prosody", &packages.Package{
			Network: packages.PackageNetwork{
				External:      packages.PortMap{5222: 5222},
				ExternalNames: packages.PortNameMap{5222: "xmpp"},
				Internal:      packages.PortMap{5347: 5347},
				InternalNames: packages.PortNameMap{5347: "component"},
			},
		})
		want := packages.TemplateDep{
			Host: "c-prosody",
			Ports: map[string]string{
				"5222": "5222", "xmpp": "5222",
				"5347": "5347", "component": "5347",
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("empty port name is not emitted", func(t *testing.T) {
		got := buildTemplateDepEntry("c-db", &packages.Package{
			Network: packages.PackageNetwork{
				External:      packages.PortMap{5432: 5432},
				ExternalNames: packages.PortNameMap{5432: ""},
			},
		})
		want := packages.TemplateDep{
			Host:  "c-db",
			Ports: map[string]string{"5432": "5432"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("no ports still returns a non-nil Ports map", func(t *testing.T) {
		got := buildTemplateDepEntry("c-db", &packages.Package{})
		if got.Host != "c-db" {
			t.Fatalf("host = %q, want c-db", got.Host)
		}
		// Non-nil Ports is important: Go's template `index` errors on a
		// nil map, and `.Ports` must be safe to range over from the yaml.
		if got.Ports == nil {
			t.Fatal("Ports is nil, want empty but non-nil")
		}
		if len(got.Ports) != 0 {
			t.Fatalf("Ports = %v, want empty", got.Ports)
		}
	})
}
