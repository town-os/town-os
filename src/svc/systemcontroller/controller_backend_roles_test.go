// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"reflect"
	"testing"
)

// systemControllerBackend was one flat forty-four-method interface. It is now
// the composition of six role interfaces, which is a naming exercise rather
// than a behavior change — and these tests are what keep it that way.
//
// The composition must stay exactly equal to the union of its parts. A method
// added to the composed interface directly, rather than to the role it belongs
// to, would put the boundary back where it was one getter at a time.

func methodSet(t reflect.Type) map[string]bool {
	out := make(map[string]bool, t.NumMethod())
	for i := range t.NumMethod() {
		out[t.Method(i).Name] = true
	}
	return out
}

func TestBackendIsExactlyTheUnionOfItsRoles(t *testing.T) {
	roles := map[string]reflect.Type{
		"storageBackend": reflect.TypeFor[storageBackend](),
		"packageBackend": reflect.TypeFor[packageBackend](),
		"authBackend":    reflect.TypeFor[authBackend](),
		"networkBackend": reflect.TypeFor[networkBackend](),
		"dnsBackend":     reflect.TypeFor[dnsBackend](),
		"serviceBackend": reflect.TypeFor[serviceBackend](),
		"processBackend": reflect.TypeFor[processBackend](),
	}

	union := map[string]string{} // method -> role that declares it
	for name, rt := range roles {
		for m := range methodSet(rt) {
			if prev, dup := union[m]; dup {
				t.Errorf("method %s is declared by both %s and %s; a method belongs to exactly one role", m, prev, name)
			}
			union[m] = name
		}
	}

	backend := methodSet(reflect.TypeFor[systemControllerBackend]())

	for m := range backend {
		if _, ok := union[m]; !ok {
			t.Errorf("systemControllerBackend declares %s directly instead of through a role interface; put it in the role it belongs to", m)
		}
	}
	for m, role := range union {
		if !backend[m] {
			t.Errorf("%s declares %s but systemControllerBackend does not embed it", role, m)
		}
	}
}

// serverBase is the production implementation. If it satisfies each role
// individually then a handler can take one role rather than the whole backend,
// which is the entire point of the split.
func TestServerBaseSatisfiesEachRoleIndividually(t *testing.T) {
	sb := &serverBase{}

	var (
		_ storageBackend = sb
		_ packageBackend = sb
		_ authBackend    = sb
		_ networkBackend = sb
		_ dnsBackend     = sb
		_ serviceBackend = sb
		_ processBackend = sb
	)

	// The compile-time assignments above ARE the assertion; there is nothing
	// to check at runtime. Name them so the compiler does not drop them and a
	// reader does not mistake them for dead code.
	t.Log("serverBase satisfies every role interface individually")
}

// A guard against the thing that produced a forty-four-method interface in the
// first place: one more getter, then one more, with nothing objecting.
//
// The number is not sacred and this test is not a reason to leave a subsystem
// unwired — it is a prompt. If the backend needs to grow past this, the
// question to answer first is whether the caller could take a role interface
// instead, and then raise the bound deliberately.
func TestBackendDoesNotGrowUnnoticed(t *testing.T) {
	const bound = 44

	got := reflect.TypeFor[systemControllerBackend]().NumMethod()
	if got > bound {
		t.Errorf("systemControllerBackend has %d methods, over the %d it was pinned at.\n"+
			"Before raising this: can the new caller take one of the role interfaces instead of the whole backend?", got, bound)
	}
}
