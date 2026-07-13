package packages

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	HostnameRegexp = regexp.MustCompile("^[a-z][a-z0-9-]*$")
	VolumeRegexp   = regexp.MustCompile("^[a-zA-Z0-9-_]+$")
	// PortNameRegexp constrains semantic port names used in
	// network.external / network.internal YAML keys. A name must start
	// with an ASCII letter so it is unambiguous relative to a numeric
	// port and may contain alphanumerics and underscores. Names are
	// case-insensitive in the emitted TOWNOS_DEP_*_PORT_<NAME> env var
	// (uppercased) but preserved as-written in the compiled output.
	PortNameRegexp = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9_]*$")
)

type OutputType string

var (
	ErrHostname        = errors.New("invalid hostname")
	ErrVolume          = errors.New("invalid volume name")
	ErrInvalidType     = errors.New("invalid output type")
	ErrInvalidPort     = errors.New("invalid port")
	ErrInvalidPortName = errors.New("invalid port name (must start with a letter and contain only letters, digits, and underscores)")
	ErrBytes           = errors.New("invalid byte size")
	ErrInvalidDuration = errors.New("invalid duration")
)

const (
	Port     OutputType = "port"
	Hostname OutputType = "hostname"
	Volume   OutputType = "volume"
	Bytes    OutputType = "bytes"
	Archive  OutputType = "archive"
	Duration OutputType = "duration"
	Secret   OutputType = "secret"
	Boolean  OutputType = "boolean"
	// Oauth is answered by completing a device flow in the install dialog rather
	// than by typing. The answer is the token the flow returns, so it validates
	// and is handled exactly like a Secret -- masked, never auto-generated, and
	// carried forward on upgrade.
	Oauth OutputType = "oauth"
)

func (o OutputType) Output(answer string) (string, error) {
	switch o {
	case Port:
		u, err := strconv.ParseUint(answer, 10, 64)
		if err != nil {
			return "", err
		}

		if u == 0 || u > 65535 {
			return "", ErrInvalidPort
		}

		return answer, nil
	case Hostname:
		if HostnameRegexp.MatchString(strings.ToLower(answer)) {
			return strings.ToLower(answer), nil
		} else {
			return "", ErrHostname
		}
	case Volume:
		if VolumeRegexp.MatchString(answer) {
			return answer, nil
		} else {
			return "", ErrVolume
		}
	case Bytes:
		b, err := ParseBytes(answer)
		if err != nil {
			return "", err
		}
		return strconv.FormatUint(b, 10), nil
	case Archive:
		if answer == "" {
			return "", ErrInvalidType
		}
		return answer, nil
	case Secret, Oauth:
		if answer == "" {
			return "", ErrInvalidType
		}
		return answer, nil
	case Boolean:
		// strconv.ParseBool accepts exactly the spellings YAML 1.2 treats as
		// booleans (plus 1/0/t/f); it is normalized to "true"/"false" so
		// template substitution produces one canonical form.
		b, err := strconv.ParseBool(strings.TrimSpace(answer))
		if err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case Duration:
		d, err := ParseDuration(answer)
		if err != nil {
			return "", err
		}
		return strconv.FormatUint(d, 10), nil
	default:
		return "", ErrInvalidType
	}
}

// ParseBytes parses a human-readable byte size string into a uint64 byte count.
// Accepted formats: pure integer (bytes), or a number followed by a suffix
// (mb, gb, tb). Suffixes are case-insensitive. Returns 0 for empty strings.
func ParseBytes(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	lower := strings.ToLower(s)

	type suffix struct {
		name       string
		multiplier uint64
	}

	suffixes := []suffix{
		{"tb", 1024 * 1024 * 1024 * 1024},
		{"gb", 1024 * 1024 * 1024},
		{"mb", 1024 * 1024},
	}

	for _, sf := range suffixes {
		if strings.HasSuffix(lower, sf.name) {
			numStr := strings.TrimSpace(s[:len(s)-len(sf.name)])
			val, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("%w: %q", ErrBytes, s)
			}
			return val * sf.multiplier, nil
		}
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrBytes, s)
	}
	return val, nil
}

// ParseDuration parses a human-readable duration string into a uint64 seconds count.
// Accepted formats: pure integer (seconds), or a number followed by a suffix
// (s, m, h, d). Suffixes are case-insensitive. Returns 0 for empty strings.
func ParseDuration(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	lower := strings.ToLower(s)

	type suffix struct {
		name       string
		multiplier uint64
	}

	suffixes := []suffix{
		{"d", 24 * 60 * 60},
		{"h", 60 * 60},
		{"m", 60},
		{"s", 1},
	}

	for _, sf := range suffixes {
		if strings.HasSuffix(lower, sf.name) {
			numStr := strings.TrimSpace(s[:len(s)-len(sf.name)])
			val, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("%w: %q", ErrInvalidDuration, s)
			}
			return val * sf.multiplier, nil
		}
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidDuration, s)
	}
	return val, nil
}
