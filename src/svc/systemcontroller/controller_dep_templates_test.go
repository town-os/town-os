// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"
)

func TestApplyDepTemplates(t *testing.T) {
	t.Run("single dep host and port", func(t *testing.T) {
		env := map[string]string{
			"DB_HOST": "@dep_db_host@",
			"DB_PORT": "@dep_db_port_5432@",
		}
		depEnvVars := map[string]string{
			"TOWNOS_DEP_DB_HOST":      "town-os-package--default-myapp-dep-db-1.0",
			"TOWNOS_DEP_DB_PORT_5432": "5432",
		}

		applyDepTemplates(env, depEnvVars)

		if env["DB_HOST"] != "town-os-package--default-myapp-dep-db-1.0" {
			t.Fatalf("expected %q, got %q", "town-os-package--default-myapp-dep-db-1.0", env["DB_HOST"])
		}
		if env["DB_PORT"] != "5432" {
			t.Fatalf("expected %q, got %q", "5432", env["DB_PORT"])
		}
	})

	t.Run("multiple deps", func(t *testing.T) {
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@@@dep_db_host@:@dep_db_port_5432@/mydb",
			"CACHE_URL":    "redis://@dep_cache_host@:@dep_cache_port_6379@",
		}
		depEnvVars := map[string]string{
			"TOWNOS_DEP_DB_HOST":         "db-container",
			"TOWNOS_DEP_DB_PORT_5432":    "5432",
			"TOWNOS_DEP_CACHE_HOST":      "cache-container",
			"TOWNOS_DEP_CACHE_PORT_6379": "6379",
		}

		applyDepTemplates(env, depEnvVars)

		if env["DATABASE_URL"] != "postgres://user:pass@db-container:5432/mydb" {
			t.Fatalf("expected %q, got %q", "postgres://user:pass@db-container:5432/mydb", env["DATABASE_URL"])
		}
		if env["CACHE_URL"] != "redis://cache-container:6379" {
			t.Fatalf("expected %q, got %q", "redis://cache-container:6379", env["CACHE_URL"])
		}
	})

	t.Run("env values without dep templates unchanged", func(t *testing.T) {
		env := map[string]string{
			"PLAIN":       "no-templates-here",
			"STATIC_PORT": "3000",
			"WITH_DEP":    "@dep_db_host@",
		}
		depEnvVars := map[string]string{
			"TOWNOS_DEP_DB_HOST": "db-container",
		}

		applyDepTemplates(env, depEnvVars)

		if env["PLAIN"] != "no-templates-here" {
			t.Fatalf("expected %q, got %q", "no-templates-here", env["PLAIN"])
		}
		if env["STATIC_PORT"] != "3000" {
			t.Fatalf("expected %q, got %q", "3000", env["STATIC_PORT"])
		}
		if env["WITH_DEP"] != "db-container" {
			t.Fatalf("expected %q, got %q", "db-container", env["WITH_DEP"])
		}
	})

	t.Run("empty dep env vars", func(t *testing.T) {
		env := map[string]string{
			"SOME_VAR": "@dep_db_host@",
		}
		depEnvVars := map[string]string{}

		applyDepTemplates(env, depEnvVars)

		if env["SOME_VAR"] != "@dep_db_host@" {
			t.Fatalf("expected %q, got %q", "@dep_db_host@", env["SOME_VAR"])
		}
	})

	t.Run("key conversion lowercases correctly", func(t *testing.T) {
		env := map[string]string{
			"URL": "http://@dep_myservice_host@:@dep_myservice_port_8080@/api",
		}
		depEnvVars := map[string]string{
			"TOWNOS_DEP_MYSERVICE_HOST":      "svc-container",
			"TOWNOS_DEP_MYSERVICE_PORT_8080": "8080",
		}

		applyDepTemplates(env, depEnvVars)

		if env["URL"] != "http://svc-container:8080/api" {
			t.Fatalf("expected %q, got %q", "http://svc-container:8080/api", env["URL"])
		}
	})

	t.Run("named port resolves alongside numeric", func(t *testing.T) {
		// When the dep declares a semantic name for a container port,
		// both TOWNOS_DEP_<KEY>_PORT_<N> and TOWNOS_DEP_<KEY>_PORT_<NAME>
		// are emitted and the parent can use either form interchangeably.
		env := map[string]string{
			"NUMERIC": "@dep_db_host@:@dep_db_port_5432@",
			"NAMED":   "@dep_db_host@:@dep_db_port_sql@",
		}
		depEnvVars := map[string]string{
			"TOWNOS_DEP_DB_HOST":      "db-container",
			"TOWNOS_DEP_DB_PORT_5432": "5432",
			"TOWNOS_DEP_DB_PORT_SQL":  "5432",
		}

		applyDepTemplates(env, depEnvVars)

		if env["NUMERIC"] != "db-container:5432" {
			t.Fatalf("expected numeric form resolved, got %q", env["NUMERIC"])
		}
		if env["NAMED"] != "db-container:5432" {
			t.Fatalf("expected named form resolved, got %q", env["NAMED"])
		}
	})

	t.Run("named port with underscore", func(t *testing.T) {
		env := map[string]string{
			"URL": "http://@dep_svc_host@:@dep_svc_port_admin_ui@/",
		}
		depEnvVars := map[string]string{
			"TOWNOS_DEP_SVC_HOST":          "svc-container",
			"TOWNOS_DEP_SVC_PORT_ADMIN_UI": "9001",
		}

		applyDepTemplates(env, depEnvVars)

		if env["URL"] != "http://svc-container:9001/" {
			t.Fatalf("expected underscore-name resolved, got %q", env["URL"])
		}
	})
}
