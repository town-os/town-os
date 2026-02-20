package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gitea.com/town-os/town-os/src/packages"
)

// ProblemError wraps a ProblemDetail with request context.
type ProblemError struct {
	Method           string
	Path             string
	Problem          ProblemDetail
	ValidationErrors []packages.ResponseValidationError
}

func (pe *ProblemError) Error() string {
	return fmt.Sprintf("%s %s: %s", pe.Method, pe.Path, pe.Problem.Detail)
}

func (pe *ProblemError) StatusCode() int {
	return pe.Problem.Status
}

// Is allows errors.Is(pe, ErrUnsuccessfulStatus) to return true
// for backward compatibility.
func (pe *ProblemError) Is(target error) bool {
	return target == ErrUnsuccessfulStatus
}

// readProblemDetail reads the response body and parses it as a problem+json
// response. Falls back to a synthesized ProblemDetail from the status code.
// Does NOT close the body — the caller's defer handles that.
func readProblemDetail(resp *http.Response, method, path string) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ProblemError{
			Method: method,
			Path:   path,
			Problem: ProblemDetail{
				Type:   fmt.Sprintf("about:blank#%d", resp.StatusCode),
				Title:  http.StatusText(resp.StatusCode),
				Status: resp.StatusCode,
				Detail: fmt.Sprintf("status %d (failed to read body: %v)", resp.StatusCode, err),
			},
		}
	}

	// Try parsing as InstallProblemDetail (has validation_errors extension).
	var ipd struct {
		ProblemDetail
		ValidationErrors []packages.ResponseValidationError `json:"validation_errors"`
	}
	if err := json.Unmarshal(body, &ipd); err == nil && ipd.Status != 0 && ipd.Detail != "" {
		return &ProblemError{
			Method:           method,
			Path:             path,
			Problem:          ipd.ProblemDetail,
			ValidationErrors: ipd.ValidationErrors,
		}
	}

	// Try legacy echo format: {"message": "..."}
	var legacy struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &legacy); err == nil && legacy.Message != "" {
		return &ProblemError{
			Method: method,
			Path:   path,
			Problem: ProblemDetail{
				Type:   fmt.Sprintf("about:blank#%d", resp.StatusCode),
				Title:  http.StatusText(resp.StatusCode),
				Status: resp.StatusCode,
				Detail: legacy.Message,
			},
		}
	}

	// Fallback
	detail := string(body)
	if detail == "" {
		detail = fmt.Sprintf("status %d", resp.StatusCode)
	}

	return &ProblemError{
		Method: method,
		Path:   path,
		Problem: ProblemDetail{
			Type:   fmt.Sprintf("about:blank#%d", resp.StatusCode),
			Title:  http.StatusText(resp.StatusCode),
			Status: resp.StatusCode,
			Detail: detail,
		},
	}
}

// readProblemDetailAndClose reads the response body, parses it as problem+json,
// and closes the body. Use this when the body is not managed by a defer.
func readProblemDetailAndClose(resp *http.Response, method, path string) error {
	err := readProblemDetail(resp, method, path)
	return errors.Join(err, resp.Body.Close())
}
