package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/labstack/echo/v5"
)

// ProblemDetail represents an RFC 9457 Problem Details object.
type ProblemDetail struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func NewProblemDetail(status int, detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   fmt.Sprintf("about:blank#%d", status),
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
}

func (p *ProblemDetail) Error() string {
	return p.Detail
}

func (p *ProblemDetail) StatusCode() int {
	return p.Status
}

func (p *ProblemDetail) MarshalJSON() ([]byte, error) {
	type alias ProblemDetail
	return json.Marshal((*alias)(p))
}

// errorToProblemDetail converts any error to a ProblemDetail.
func errorToProblemDetail(err error) *ProblemDetail {
	var pd *ProblemDetail
	if errors.As(err, &pd) {
		return pd
	}

	var he *echo.HTTPError
	if errors.As(err, &he) {
		detail := he.Message
		if detail == "" {
			detail = he.Error()
		}
		return NewProblemDetail(he.Code, detail)
	}

	var sc echo.HTTPStatusCoder
	if errors.As(err, &sc) {
		return NewProblemDetail(sc.StatusCode(), err.Error())
	}

	return NewProblemDetail(http.StatusInternalServerError, err.Error())
}

var sensitivePattern = regexp.MustCompile(`(?i)(password|credential|secret|token)["']?\s*[:=]\s*["']?[^\s,"'}\]]+`)

// sanitizeProblemDetail scrubs sensitive values from the detail string.
func sanitizeProblemDetail(pd *ProblemDetail) *ProblemDetail {
	pd.Detail = sensitivePattern.ReplaceAllStringFunc(pd.Detail, func(match string) string {
		idx := -1
		for i, ch := range match {
			if ch == ':' || ch == '=' {
				idx = i
				break
			}
		}
		if idx < 0 {
			return match
		}
		return fmt.Sprintf("%s[REDACTED]", match[:idx+1])
	})
	return pd
}

// ProblemDetailHTTPErrorHandler returns a custom echo.HTTPErrorHandler
// that writes RFC 9457 problem+json responses.
func ProblemDetailHTTPErrorHandler() echo.HTTPErrorHandler {
	return func(c *echo.Context, err error) {
		if r, _ := echo.UnwrapResponse(c.Response()); r != nil && r.Committed {
			return
		}

		pd := errorToProblemDetail(err)
		pd = sanitizeProblemDetail(pd)

		c.Response().Header().Set("Content-Type", "application/problem+json")
		c.Response().WriteHeader(pd.Status)
		_ = json.NewEncoder(c.Response()).Encode(pd)
	}
}
