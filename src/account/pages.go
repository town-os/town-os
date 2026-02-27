package account

import (
	"errors"
	"time"
)

var (
	ErrPageNotFound       = errors.New("page not found")
	ErrDuplicatePageName  = errors.New("page name already exists")
	ErrPageNameRequired   = errors.New("page name is required")
	ErrPageRepoRequired   = errors.New("page repository URL is required")
	ErrPageDomainRequired = errors.New("page domain is required")
)

type PageSite struct {
	Name      string    `json:"name"`
	RepoURL   string    `json:"repo_url"`
	Branch    string    `json:"branch"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PageSiteUpdate struct {
	RepoURL *string `json:"repo_url,omitempty"`
	Branch  *string `json:"branch,omitempty"`
	Domain  *string `json:"domain,omitempty"`
	Status  *string `json:"status,omitempty"`
}

type PagesManager interface {
	Create(name, repoURL, branch, domain string) (*PageSite, error)
	Get(name string) (*PageSite, error)
	Update(name string, fields PageSiteUpdate) (*PageSite, error)
	Remove(name string) error
	List() ([]PageSite, error)
}
