// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"reflect"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestEmitDepPortEnv(t *testing.T) {
	t.Run("numeric only emits numeric form", func(t *testing.T) {
		env := map[string]string{}
		emitDepPortEnv(env, "DB", packages.PortMap{5432: 5432}, nil)

		want := map[string]string{
			"TOWNOS_DEP_DB_PORT_5432": "5432",
		}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %v, want %v", env, want)
		}
	})

	t.Run("named emits both numeric and named", func(t *testing.T) {
		env := map[string]string{}
		emitDepPortEnv(env, "DB", packages.PortMap{5432: 5432}, packages.PortNameMap{5432: "sql"})

		want := map[string]string{
			"TOWNOS_DEP_DB_PORT_5432": "5432",
			"TOWNOS_DEP_DB_PORT_SQL":  "5432",
		}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %v, want %v", env, want)
		}
	})

	t.Run("named uppercases the name", func(t *testing.T) {
		env := map[string]string{}
		emitDepPortEnv(env, "SVC", packages.PortMap{8080: 8080}, packages.PortNameMap{8080: "adminUI"})

		if env["TOWNOS_DEP_SVC_PORT_ADMINUI"] != "8080" {
			t.Fatalf("expected uppercased name key, got env=%v", env)
		}
	})

	t.Run("multiple ports mix named and unnamed", func(t *testing.T) {
		env := map[string]string{}
		// Only 5432 has a name; 6379 is unnamed.
		emitDepPortEnv(env, "CACHE",
			packages.PortMap{5432: 5432, 6379: 6379},
			packages.PortNameMap{5432: "sql"},
		)

		if env["TOWNOS_DEP_CACHE_PORT_5432"] != "5432" {
			t.Fatalf("expected numeric form for 5432, got %v", env)
		}
		if env["TOWNOS_DEP_CACHE_PORT_SQL"] != "5432" {
			t.Fatalf("expected named form for sql, got %v", env)
		}
		if env["TOWNOS_DEP_CACHE_PORT_6379"] != "6379" {
			t.Fatalf("expected numeric form for 6379, got %v", env)
		}
		// 6379 has no name so no TOWNOS_DEP_CACHE_PORT_<NAME> for it.
		if len(env) != 3 {
			t.Fatalf("expected exactly 3 entries, got %v", env)
		}
	})

	t.Run("empty port map emits nothing", func(t *testing.T) {
		env := map[string]string{}
		emitDepPortEnv(env, "EMPTY", packages.PortMap{}, nil)
		if len(env) != 0 {
			t.Fatalf("expected no entries, got %v", env)
		}
	})

	t.Run("empty name in map is skipped", func(t *testing.T) {
		// Defensive: an empty-string name is still skipped so it does not
		// produce a TOWNOS_DEP_KEY_PORT_ (trailing underscore) key.
		env := map[string]string{}
		emitDepPortEnv(env, "X",
			packages.PortMap{80: 80},
			packages.PortNameMap{80: ""},
		)
		if _, ok := env["TOWNOS_DEP_X_PORT_"]; ok {
			t.Fatalf("empty-string name must not produce a trailing-underscore key, got %v", env)
		}
		if env["TOWNOS_DEP_X_PORT_80"] != "80" {
			t.Fatalf("expected numeric form 80, got %v", env)
		}
	})
}
