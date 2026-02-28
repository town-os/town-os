package packages

import "time"

// TimezoneOffset returns the current UTC offset in minutes for the local
// system clock. Positive values are east of UTC, negative values are west.
// For example, a system in UTC-5 returns -300, one in UTC+5:30 returns 330.
// The UI provides a timezone selection and sends the offset; the
// Control Plane Service uses this function when it needs the local offset.
func TimezoneOffset() int {
	_, offset := time.Now().Zone()
	return offset / 60
}
