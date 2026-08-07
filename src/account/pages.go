package account

import (
	"errors"
	"fmt"
	"strings"
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
	// ErrPageInvalidDomain is returned for a domain that is not a hostname.
	//
	// It matters more than a format complaint. A page's domain is not a label:
	// it names the page's btrfs subvolume, its webroot symlink, the directory
	// the static server roots on, its leaf certificate's SAN, its DANE TLSA
	// owner, and its ingress vhost. filepath.Join collapses "..", so a domain
	// carrying one addresses a sibling of the pages tree under the btrfs base —
	// where the local CA's private key, the installed packages and the system
	// database live — and a domain carrying whitespace or braces restructures
	// the rendered Caddyfile.
	ErrPageInvalidDomain = errors.New("page domain must be a hostname")
)

// ValidatePageDomain reports whether a domain is a hostname Town OS can serve.
//
// Deliberately strict, and checked where the value enters rather than at each
// of the six places that consume it: a domain reaching any one of them
// unvalidated is a bug in whichever consumer forgot, and there is no shape a
// legitimate page needs that this refuses. Labels are letters, digits and
// dashes, not starting or ending with a dash; a trailing dot is tolerated
// because a fully-qualified name may carry one and pageHostname strips it.
//
// No underscore. It is not a hostname character (RFC 1123) and it cannot appear
// in a certificate SAN, so a page named with one would be served on a vhost the
// local CA cannot issue a valid leaf for -- the page would resolve and then
// fail the handshake. A page created before this check that uses one keeps
// working until its domain is next edited, at which point it has to be
// corrected.
func ValidatePageDomain(domain string) error {
	d := strings.TrimSpace(domain)
	if d == "" {
		return ErrPageDomainRequired
	}
	if len(d) > 253 {
		return fmt.Errorf("%w: longer than 253 characters", ErrPageInvalidDomain)
	}
	// A single trailing dot is the root label; anything else ending in a dot is
	// an empty label.
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return fmt.Errorf("%w: %q", ErrPageInvalidDomain, domain)
	}
	for label := range strings.SplitSeq(d, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("%w: %q has an empty or over-long label", ErrPageInvalidDomain, domain)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("%w: %q has a label starting or ending with a dash", ErrPageInvalidDomain, domain)
		}
		for i := range len(label) {
			c := label[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
			default:
				return fmt.Errorf("%w: %q contains %q", ErrPageInvalidDomain, domain, string(c))
			}
		}
	}
	return nil
}

type PageSite struct {
	Name           string    `json:"name"`
	RepoURL        string    `json:"repo_url"`
	Branch         string    `json:"branch"`
	Domain         string    `json:"domain"`
	SourceType     string    `json:"source_type"`
	Image          string    `json:"image"`
	ImageDirectory string    `json:"image_directory"`
	Status         string    `json:"status"`
	// Network is the network the page is published on, exactly like a package's
	// install network: it selects the TLD the page's hostname, leaf SAN, DANE
	// TLSA and ingress vhost are all named under, and it decides who can resolve
	// the page. Empty (the zero value, and the DB default) means the default/home
	// network — the same convention as Installer.LoadNetwork, which returns ""
	// for a default-network package. A page on a non-default network is
	// dual-homed (scoped overlay record for WireGuard peers + global record at
	// the LAN IP) and is hidden from peers of every OTHER network.
	Network   string    `json:"network"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PageSiteUpdate struct {
	RepoURL        *string `json:"repo_url,omitempty"`
	Branch         *string `json:"branch,omitempty"`
	Domain         *string `json:"domain,omitempty"`
	SourceType     *string `json:"source_type,omitempty"`
	Image          *string `json:"image,omitempty"`
	ImageDirectory *string `json:"image_directory,omitempty"`
	Status         *string `json:"status,omitempty"`
	Network        *string `json:"network,omitempty"`
}

// ValidPageSourceType returns true if the given source type is valid.
func ValidPageSourceType(s string) bool {
	return s == PageSourceArchive || s == PageSourceContainerImage || s == PageSourceGit
}

type PagesManager interface {
	// Create registers a page. network is the network it is published on; ""
	// means the default/home network (see PageSite.Network).
	Create(name, repoURL, branch, domain, sourceType, image, imageDirectory, network string) (*PageSite, error)
	Get(name string) (*PageSite, error)
	Update(name string, fields PageSiteUpdate) (*PageSite, error)
	Remove(name string) error
	List() ([]PageSite, error)
}
