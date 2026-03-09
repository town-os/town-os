// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"
	"time"
)

type sortTestStruct struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Score uint64 `json:"score"`
	Admin bool   `json:"admin"`
}

type sortTestTime struct {
	Label string    `json:"label"`
	When  time.Time `json:"when"`
}

type sortTestNoTag struct {
	Name  string
	Value string
}

// --- Basic ascending/descending ---

func TestSortSliceStringAsc(t *testing.T) {
	data := []sortTestStruct{
		{Name: "charlie"},
		{Name: "alice"},
		{Name: "bob"},
	}

	sortSlice(data, "name", "asc")

	expected := []string{"alice", "bob", "charlie"}
	for i, want := range expected {
		if data[i].Name != want {
			t.Fatalf("index %d: expected %q, got %q", i, want, data[i].Name)
		}
	}
}

func TestSortSliceStringDesc(t *testing.T) {
	data := []sortTestStruct{
		{Name: "alice"},
		{Name: "charlie"},
		{Name: "bob"},
	}

	sortSlice(data, "name", "desc")

	expected := []string{"charlie", "bob", "alice"}
	for i, want := range expected {
		if data[i].Name != want {
			t.Fatalf("index %d: expected %q, got %q", i, want, data[i].Name)
		}
	}
}

// --- Int sorting ---

func TestSortSliceIntAsc(t *testing.T) {
	data := []sortTestStruct{
		{Name: "c", Count: 30},
		{Name: "a", Count: 10},
		{Name: "b", Count: 20},
	}

	sortSlice(data, "count", "asc")

	if data[0].Count != 10 || data[1].Count != 20 || data[2].Count != 30 {
		t.Fatalf("expected sorted [10, 20, 30], got [%d, %d, %d]", data[0].Count, data[1].Count, data[2].Count)
	}
}

func TestSortSliceIntDesc(t *testing.T) {
	data := []sortTestStruct{
		{Name: "a", Count: 10},
		{Name: "c", Count: 30},
		{Name: "b", Count: 20},
	}

	sortSlice(data, "count", "desc")

	if data[0].Count != 30 || data[1].Count != 20 || data[2].Count != 10 {
		t.Fatalf("expected sorted [30, 20, 10], got [%d, %d, %d]", data[0].Count, data[1].Count, data[2].Count)
	}
}

// --- Uint sorting ---

func TestSortSliceUintAsc(t *testing.T) {
	data := []sortTestStruct{
		{Name: "c", Score: 300},
		{Name: "a", Score: 100},
		{Name: "b", Score: 200},
	}

	sortSlice(data, "score", "asc")

	if data[0].Score != 100 || data[1].Score != 200 || data[2].Score != 300 {
		t.Fatalf("expected sorted [100, 200, 300], got [%d, %d, %d]", data[0].Score, data[1].Score, data[2].Score)
	}
}

// --- Bool sorting ---

func TestSortSliceBoolAsc(t *testing.T) {
	data := []sortTestStruct{
		{Name: "admin", Admin: true},
		{Name: "user", Admin: false},
		{Name: "admin2", Admin: true},
	}

	sortSlice(data, "admin", "asc")

	if data[0].Admin != false {
		t.Fatalf("expected first element Admin=false, got true")
	}
	if data[1].Admin != true || data[2].Admin != true {
		t.Fatalf("expected last two elements Admin=true")
	}
}

// --- Time sorting ---

func TestSortSliceTimeAsc(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

	data := []sortTestTime{
		{Label: "c", When: t3},
		{Label: "a", When: t1},
		{Label: "b", When: t2},
	}

	sortSlice(data, "when", "asc")

	if !data[0].When.Equal(t1) || !data[1].When.Equal(t2) || !data[2].When.Equal(t3) {
		t.Fatalf("time sort asc failed: got %v, %v, %v", data[0].When, data[1].When, data[2].When)
	}
}

func TestSortSliceTimeDesc(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

	data := []sortTestTime{
		{Label: "a", When: t1},
		{Label: "c", When: t3},
		{Label: "b", When: t2},
	}

	sortSlice(data, "when", "desc")

	if !data[0].When.Equal(t3) || !data[1].When.Equal(t2) || !data[2].When.Equal(t1) {
		t.Fatalf("time sort desc failed: got %v, %v, %v", data[0].When, data[1].When, data[2].When)
	}
}

// --- No json tag: match by field name ---

func TestSortSliceNoJsonTag(t *testing.T) {
	data := []sortTestNoTag{
		{Name: "charlie", Value: "3"},
		{Name: "alice", Value: "1"},
		{Name: "bob", Value: "2"},
	}

	sortSlice(data, "Name", "asc")

	expected := []string{"alice", "bob", "charlie"}
	for i, want := range expected {
		if data[i].Name != want {
			t.Fatalf("index %d: expected %q, got %q", i, want, data[i].Name)
		}
	}
}

// --- Edge cases ---

func TestSortSliceEmptySlice(t *testing.T) {
	data := []sortTestStruct{}
	result := sortSlice(data, "name", "asc")
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(result))
	}
}

func TestSortSliceEmptySortBy(t *testing.T) {
	data := []sortTestStruct{
		{Name: "charlie"},
		{Name: "alice"},
	}

	result := sortSlice(data, "", "asc")

	// Should return unchanged
	if result[0].Name != "charlie" || result[1].Name != "alice" {
		t.Fatal("expected slice unchanged when sortBy is empty")
	}
}

func TestSortSliceUnknownField(t *testing.T) {
	data := []sortTestStruct{
		{Name: "charlie"},
		{Name: "alice"},
	}

	result := sortSlice(data, "nonexistent", "asc")

	// Should return unchanged
	if result[0].Name != "charlie" || result[1].Name != "alice" {
		t.Fatal("expected slice unchanged when sortBy field not found")
	}
}

func TestSortSliceDefaultDirection(t *testing.T) {
	data := []sortTestStruct{
		{Name: "charlie"},
		{Name: "alice"},
		{Name: "bob"},
	}

	// Non-standard direction should default to asc
	sortSlice(data, "name", "invalid")

	expected := []string{"alice", "bob", "charlie"}
	for i, want := range expected {
		if data[i].Name != want {
			t.Fatalf("index %d: expected %q, got %q", i, want, data[i].Name)
		}
	}
}

func TestSortSliceStable(t *testing.T) {
	data := []sortTestStruct{
		{Name: "alice", Count: 1},
		{Name: "alice", Count: 2},
		{Name: "alice", Count: 3},
	}

	sortSlice(data, "name", "asc")

	// Stable sort should preserve original order for equal elements
	for i, want := range []int{1, 2, 3} {
		if data[i].Count != want {
			t.Fatalf("stable sort broken at index %d: expected count %d, got %d", i, want, data[i].Count)
		}
	}
}

func TestSortSlicePinnedValue(t *testing.T) {
	data := []sortTestStruct{
		{Name: "charlie", Count: 3},
		{Name: "alice", Count: 1},
		{Name: "bob", Count: 2},
		{Name: "alice", Count: 4},
	}

	// Pin "bob" to the top, rest sorted alphabetically
	sortSlice(data, "name", "bob")

	if data[0].Name != "bob" {
		t.Fatalf("expected pinned value 'bob' first, got %q", data[0].Name)
	}
	// Rest should be sorted alphabetically
	if data[1].Name != "alice" || data[2].Name != "alice" || data[3].Name != "charlie" {
		t.Fatalf("expected rest sorted alphabetically, got %q %q %q", data[1].Name, data[2].Name, data[3].Name)
	}
}

func TestSortSlicePinnedValueCaseInsensitive(t *testing.T) {
	data := []sortTestStruct{
		{Name: "active", Count: 1},
		{Name: "failed", Count: 2},
		{Name: "inactive", Count: 3},
		{Name: "active", Count: 4},
	}

	// Pin "Failed" (case insensitive) to the top
	sortSlice(data, "name", "Failed")

	if data[0].Name != "failed" {
		t.Fatalf("expected pinned value 'failed' first, got %q", data[0].Name)
	}
}
