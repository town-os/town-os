package packages

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const (
	HostnameRegexp = "^[a-z][a-z0-9-]*$"
	VolumeRegexp   = "^[a-z][a-z0-9-_]*$"
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

type Responses map[string]string

type Prompt struct {
	Question string     `yaml:"question"`
	Type     OutputType `yaml:"type"`
}

func (o OutputType) Output(answer string) (any, error) {
	switch o {
	case Port:
		u, err := strconv.ParseUint(answer, 10, 64)
		if err != nil {
			return nil, err
		}

		if u >= 65535 {
			return 0, ErrInvalidPort
		}

		return uint16(u), err
	case Hostname:
		if matched, err := regexp.MatchString(HostnameRegexp, strings.ToLower(answer)); err == nil && matched {
			return answer, nil
		} else {
			return "", ErrHostname
		}
	case Volume:
		if matched, err := regexp.MatchString(VolumeRegexp, strings.ToLower(answer)); err == nil && matched {
			return answer, nil
		} else {
			return "", ErrVolume
		}
	default:
		return "", ErrInvalidType
	}
}
