package account

import (
	"errors"
	"time"
)

const (
	// PageSourceArchive indicates the page content comes from an uploaded archive.
	// This is the default source type for new pages.
	PageSourceArchive = "archive"
	// PageSourceContainerImage indicates the page content comes from a container image.
	PageSourceContainerImage = "container_image"
	// PageSourceGit indicates the page content comes from a git repository.
	PageSourceGit = "git"
)

var (
	ErrPageNotFound              = errors.New("page not found")
	ErrDuplicatePageName         = errors.New("page name already exists")
	ErrPageNameRequired          = errors.New("page name is required")
	ErrPageRepoRequired          = errors.New("page repository URL is required")
	ErrPageDomainRequired        = errors.New("page domain is required")
	ErrPageImageRequired         = errors.New("page container image is required")
	ErrPageImageDirectoryRequired = errors.New("page image directory is required")
	ErrPageInvalidSourceType     = errors.New("page source type must be archive, container_image, or git")
)

type PageSite struct {
	Name           string    `json:"name"`
	RepoURL        string    `json:"repo_url"`
	Branch         string    `json:"branch"`
	Domain         string    `json:"domain"`
	SourceType     string    `json:"source_type"`
	Image          string    `json:"image"`
	ImageDirectory string    `json:"image_directory"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PageSiteUpdate struct {
	RepoURL        *string `json:"repo_url,omitempty"`
	Branch         *string `json:"branch,omitempty"`
	Domain         *string `json:"domain,omitempty"`
	SourceType     *string `json:"source_type,omitempty"`
	Image          *string `json:"image,omitempty"`
	ImageDirectory *string `json:"image_directory,omitempty"`
	Status         *string `json:"status,omitempty"`
}

// ValidPageSourceType returns true if the given source type is valid.
func ValidPageSourceType(s string) bool {
	return s == PageSourceArchive || s == PageSourceContainerImage || s == PageSourceGit
}

type PagesManager interface {
	Create(name, repoURL, branch, domain, sourceType, image, imageDirectory string) (*PageSite, error)
	Get(name string) (*PageSite, error)
	Update(name string, fields PageSiteUpdate) (*PageSite, error)
	Remove(name string) error
	List() ([]PageSite, error)
}
