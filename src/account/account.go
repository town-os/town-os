// Package account defines user account types, validation, and the [Manager]
// interface used by the Control Plane Service.
package account

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Sentinel errors returned by [Manager] implementations.
var (
	ErrNotFound           = errors.New("account not found")
	ErrDuplicateUsername  = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrMissingContactInfo = errors.New("email, phone, and real name are required")
	ErrAccountDisabled    = errors.New("account is disabled")
	ErrInvalidEmail       = errors.New("invalid email address")
	ErrInvalidPhone       = errors.New("invalid phone number")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	// ErrPasswordInvalidChars is returned when a password contains any
	// byte outside the visible ASCII range 0x21..0x7E. Transports on the
	// path between the browser and bcrypt (HTTP Basic auth, JSON, URL
	// encoding, the DB's latin1 columns) all mishandle high-bit or
	// control bytes in subtly different ways, so the safest policy is to
	// reject them at creation time rather than trust every layer to
	// round-trip them correctly.
	ErrPasswordInvalidChars = errors.New("password may only contain printable ASCII characters (no spaces)")
	// ErrInvalidNetworkName is returned when a scoped network name is not a
	// legal network identifier.
	ErrInvalidNetworkName = errors.New("invalid network name in account scope")
)

var emailRegexp = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
var phoneRegexp = regexp.MustCompile(`^\+?[\d\s\-().]*\d[\d\s\-().]*$`)

// Account represents a user account in the system. The Disabled field
// controls whether the user can authenticate; Admin controls whether the
// user has administrative privileges.
type Account struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Email        string    `json:"email"`
	Phone        string    `json:"phone"`
	RealName     string    `json:"real_name"`
	Admin        bool      `json:"admin"`
	Disabled     bool      `json:"disabled"`
	// Grants are the named capabilities this account holds, from AllGrants.
	//
	// Toggles, not a kind: an account is an administrator — which holds every
	// grant, present and future — or it is not, and a non-admin carries
	// whichever of these are switched on. Empty is an ordinary dashboard
	// account. Unlike Admin, Grants is mutable after creation.
	Grants []string `json:"grants"`
	// Networks is the set of networks this account's grants apply to. It is
	// meaningful only when Grants is non-empty, and must be non-empty then. An
	// empty list is never "any network".
	Networks  []string  `json:"networks"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpdateFields holds optional fields for updating an account. Only non-nil
// pointer fields are applied during an update. Password must be at least
// 8 characters; Email must match a standard email pattern; Phone must
// contain digits with optional formatting characters.
type UpdateFields struct {
	Password *string `json:"password,omitempty"`
	Email    *string `json:"email,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	RealName *string `json:"real_name,omitempty"`
	Admin    *bool   `json:"admin,omitempty"`
	// Grants replaces the account's grant set wholesale. A nil pointer leaves
	// it untouched; a non-nil one is the new set, so clearing is an empty
	// slice rather than a second field. Every name must be in AllGrants, and a
	// non-empty set requires a non-empty network scope.
	Grants *[]string `json:"grants,omitempty"`
	// Networks replaces the account's network scope. A nil pointer leaves the
	// stored scope untouched; a non-nil pointer replaces it wholesale.
	Networks *[]string `json:"networks,omitempty"`
}

// Manager defines the interface for account CRUD and authentication operations.
type Manager interface {
	// Create creates a new user account. The password must be at least
	// 8 characters. Email, phone, and realName are all required. When admin
	// is true the account receives administrator privileges.
	Create(username, password, email, phone, realName string, admin bool) (*Account, error)
	// CreateGranted creates a non-admin account holding grants, scoped to
	// networks.
	//
	// A separate method rather than more positional arguments on Create so the
	// dozens of existing call sites are untouched, and so the security-relevant
	// invariants — such an account is never an admin, every grant is known, and
	// the scope is never empty — are enforced in one place at creation time.
	CreateGranted(username, password, email, phone, realName string, grants, networks []string) (*Account, error)
	// Get retrieves an account by username. Returns [ErrNotFound] if the
	// account does not exist.
	Get(username string) (*Account, error)
	// Update applies the non-nil fields in UpdateFields to the named account.
	// Returns the updated account or [ErrNotFound].
	Update(username string, fields UpdateFields) (*Account, error)
	// Disable prevents the named user from authenticating.
	Disable(username string) error
	// Enable re-enables a previously disabled account.
	Enable(username string) error
	// List returns all accounts.
	List() ([]Account, error)
	// Authenticate validates credentials and returns the account on success.
	// Returns [ErrInvalidCredentials] on failure or [ErrAccountDisabled] if
	// the account is disabled.
	Authenticate(username, password string) (*Account, error)
}

func validateEmail(email string) error {
	if !emailRegexp.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}

func validatePhone(phone string) error {
	if !phoneRegexp.MatchString(phone) {
		return ErrInvalidPhone
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}
	// Iterate by byte (not rune): any UTF-8 multi-byte lead is >= 0xC0
	// so a byte-range check is enough to reject every non-ASCII input
	// without having to decode runes. The allowed band 0x21..0x7E is
	// the full "visible ASCII" set — letters, digits, and punctuation —
	// minus space (0x20), DEL (0x7F), every control character, and the
	// entire high-bit plane.
	for i := range len(password) {
		b := password[i]
		if b < 0x21 || b > 0x7E {
			return ErrPasswordInvalidChars
		}
	}
	return nil
}

func validateContactInfo(email, phone, realName string) error {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(phone) == "" || strings.TrimSpace(realName) == "" {
		return ErrMissingContactInfo
	}
	err := validateEmail(email)
	if err != nil {
		return err
	}
	err = validatePhone(phone)
	if err != nil {
		return err
	}
	return nil
}

func validateUpdateFields(fields UpdateFields) error {
	if fields.Password != nil {
		err := validatePassword(*fields.Password)
		if err != nil {
			return err
		}
	}
	if fields.Email != nil {
		err := validateEmail(*fields.Email)
		if err != nil {
			return err
		}
	}
	if fields.Phone != nil {
		err := validatePhone(*fields.Phone)
		if err != nil {
			return err
		}
	}
	if fields.Grants != nil {
		if err := validateGrants(*fields.Grants); err != nil {
			return err
		}
	}
	if fields.Networks != nil {
		// Only the *names* are validated here, not the non-emptiness. Clearing
		// the scope to empty is legal on an update — e.g. dropping every grant
		// in the same call. Whether the *resulting* account may have an empty
		// scope is decided by validateGrantResult (SQLite) / the mock's
		// resolution, which know the post-update grant set; enforcing
		// non-emptiness here would reject that legitimate case.
		if err := validateNetworkNames(*fields.Networks); err != nil {
			return err
		}
	}
	return nil
}

// validateNetworkScope checks that a grant-holding account's network list is
// non-empty and every entry is a legal network name. It is deliberately strict:
// a malformed or empty scope on such an account is a fail-open risk, so it is
// rejected at the boundary rather than stored and interpreted later. Used on
// the create path, where the account holds grants by construction.
func validateNetworkScope(networks []string) error {
	if len(networks) == 0 {
		return ErrGrantsNoNetworks
	}
	return validateNetworkNames(networks)
}

// validateNetworkNames checks that every entry is a legal network name, without
// any opinion on whether the list may be empty. The emptiness rule is
// context-dependent (only a grant-holding account must be non-empty), so it
// lives with the code that knows that context.
func validateNetworkNames(networks []string) error {
	for _, n := range networks {
		if !ValidNetworkName(n) {
			return ErrInvalidNetworkName
		}
	}
	return nil
}

// normalizeNetworkScope de-duplicates and sorts a network list so the stored
// scope is canonical regardless of the order the caller supplied.
func normalizeNetworkScope(networks []string) []string {
	seen := make(map[string]struct{}, len(networks))
	out := make([]string, 0, len(networks))
	for _, n := range networks {
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
