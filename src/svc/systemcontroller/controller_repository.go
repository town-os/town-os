package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/url"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

type AddRepositoryRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"` //nolint:gosec // G117 -- request field, not hardcoded
}

type RepositoryNameRequest struct {
	Name string `json:"name"`
}

type MoveRepositoryRequest struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type RepositoryInfo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Error    string `json:"error,omitempty"`
}

// --- Repository handlers ---

func (s *SystemControllerHandlers) addRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := AddRepositoryRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	u, err := url.Parse(req.URL)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(s.getLocale(), i18n.MsgRepoInvalidURL), err)
	}

	if req.Username == "" && req.Password == "" {
		req.Username, req.Password = s.Controller.GetDefaultRepoCredentials()
	}

	rr := s.Controller.GetRepositoryRoot()

	repo, err := packages.NewRepository(rr.BaseDir, req.Name, *u, req.Username, req.Password, rr.Git)
	if err != nil {
		return err
	}

	if err := rr.Add(*repo); err != nil {
		return err
	}

	rr.ForceRefresh()

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) removeRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RepositoryNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	if err := rr.Remove(req.Name); err != nil {
		return err
	}

	rr.ForceRefresh()

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) moveRepository(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := MoveRepositoryRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	if err := rr.Move(req.Name, req.Position); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) refreshRepositories(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()

	pw := NewProgressWriter(c)
	pw.Step("refreshing")

	rr.ForceRefresh()

	errs := rr.RefreshErrors()
	if len(errs) > 0 {
		pw.Err(fmt.Errorf("refresh errors: %v", errs))
		return nil
	}

	pw.Done()
	return nil
}

func (s *SystemControllerHandlers) listRepositories(c *echo.Context) error {
	rr := s.Controller.GetRepositoryRoot()
	// Defensive, in the same spirit as the pages handlers' nil-manager guard: a
	// real boot always has a repository root (step 10, long before the router is
	// built), but RefreshErrors() below returns rr.Errors — a field deref that
	// panics on a nil receiver, which drops the connection and surfaces to the
	// caller as a bare EOF rather than any status code. An unconfigured
	// repository root means no repositories, which is what an empty page says.
	if rr == nil {
		p := readListParams(c)
		return c.JSON(200, paginate([]RepositoryInfo{}, p.Limit, p.Offset))
	}

	repos, err := rr.List()
	if err != nil {
		return err
	}

	errs := rr.RefreshErrors()
	out := make([]RepositoryInfo, len(repos))
	for i, r := range repos {
		out[i] = RepositoryInfo{Name: r.Name, URL: r.URL.String(), Username: r.Username, Error: errs[r.Name]}
	}

	p := readListParams(c)
	out = filterSearch(out, p.Search)
	sortSlice(out, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(out, p.Limit, p.Offset))
}
