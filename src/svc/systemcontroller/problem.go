package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

// ProblemDetailError represents an RFC 9457 Problem Details object.
type ProblemDetailError struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func NewProblemDetail(status int, detail string) *ProblemDetailError {
	return &ProblemDetailError{
		Type:   fmt.Sprintf("about:blank#%d", status),
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
}

func (p *ProblemDetailError) Error() string {
	return p.Detail
}

func (p *ProblemDetailError) StatusCode() int {
	return p.Status
}

// InstallProblemDetailError extends ProblemDetailError with per-response validation errors.
type InstallProblemDetailError struct {
	ProblemDetailError

	ValidationErrors []packages.ResponseValidationError `json:"validation_errors"`
}

func (p *InstallProblemDetailError) MarshalJSON() ([]byte, error) {
	type alias InstallProblemDetailError
	return json.Marshal((*alias)(p))
}

// errorToProblemDetail converts any error to a ProblemDetailError.
func errorToProblemDetail(err error) *ProblemDetailError {
	var pd *ProblemDetailError
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
func sanitizeProblemDetail(pd *ProblemDetailError) *ProblemDetailError {
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
		return match[:idx+1] + "[REDACTED]"
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

		// Check for validation errors and produce an extended response.
		var ve *packages.ValidationError
		if errors.As(err, &ve) {
			ipd := &InstallProblemDetailError{
				ProblemDetailError: ProblemDetailError{
					Type:   "about:blank#422",
					Title:  "Unprocessable Entity",
					Status: http.StatusUnprocessableEntity,
					Detail: ve.Error(),
				},
				ValidationErrors: ve.Errors,
			}
			c.Response().Header().Set("Content-Type", "application/problem+json")
			c.Response().WriteHeader(ipd.Status)
			if err := json.NewEncoder(c.Response()).Encode(ipd); err != nil {
				slog.Debug("encode problem detail", "error", err)
			}
			return
		}

		pd := errorToProblemDetail(err)
		pd = sanitizeProblemDetail(pd)

		c.Response().Header().Set("Content-Type", "application/problem+json")
		c.Response().WriteHeader(pd.Status)
		if err := json.NewEncoder(c.Response()).Encode(pd); err != nil {
			slog.Debug("encode problem detail", "error", err)
		}
	}
}
