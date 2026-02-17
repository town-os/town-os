package packages

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var (
	HostnameRegexp = regexp.MustCompile("^[a-z][a-z0-9-]*$")
	VolumeRegexp   = regexp.MustCompile("^[a-zA-Z0-9-_]+$")
)

type OutputType string

var (
	ErrHostname    = errors.New("invalid hostname")
	ErrVolume      = errors.New("invalid volume name")
	ErrInvalidType = errors.New("invalid output type")
	ErrInvalidPort = errors.New("invalid port")
)

const (
	Port     OutputType = "port"
	Hostname OutputType = "hostname"
	Volume   OutputType = "volume"
)

func (o OutputType) Output(answer string) (any, error) {
	switch o {
	case Port:
		u, err := strconv.ParseUint(answer, 10, 64)
		if err != nil {
			return nil, err
		}

		if u == 0 || u > 65535 {
			return 0, ErrInvalidPort
		}

		return uint16(u), err
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
	default:
		return "", ErrInvalidType
	}
}
