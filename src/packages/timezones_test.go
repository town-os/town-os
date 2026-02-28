package packages

import "testing"

func TestTimezoneOffsetReturnsInteger(t *testing.T) {
	offset := TimezoneOffset()
	// The offset should be within a valid range: UTC-12 to UTC+14.
	if offset < -720 || offset > 840 {
		t.Fatalf("offset %d outside valid range [-720, 840]", offset)
	}
}

func TestTimezoneOffsetIsMultipleOf15(t *testing.T) {
	offset := TimezoneOffset()
	// All real-world UTC offsets are multiples of 15 minutes.
	if offset%15 != 0 {
		t.Fatalf("offset %d is not a multiple of 15", offset)
	}
}
