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
	// ErrWireGuardNoNetworks is returned when a WireGuard-only account is
	// created or updated without at least one network. A WireGuard account's
	// entire purpose is to enroll peers on specific networks; one with no
	// networks could authenticate but do nothing, and — worse — an empty
	// network list must never be read as "any network".
	ErrWireGuardNoNetworks = errors.New("a wireguard-only account must be scoped to at least one network")
	// ErrInvalidNetworkName is returned when a scoped network name is not a
	// legal network identifier.
	ErrInvalidNetworkName = errors.New("invalid network name in wireguard account scope")
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
	// WireGuard marks an account whose privileges are restricted to enrolling
	// on the WireGuard networks named in Networks and nothing else. It is a
	// fail-closed capability, enforced by wireGuardAllowlistMiddleware: such an
	// account is denied every endpoint that is not on the allowlist, so it
	// cannot reach the control plane the way a normal (non-admin) account can.
	// Unlike Admin, WireGuard is mutable after creation.
	WireGuard bool `json:"wireguard"`
	// Networks is the set of networks a WireGuard account may enroll peers on.
	// It is meaningful only when WireGuard is true, and must be non-empty then.
	// An empty list is never "any network".
	Networks []string `json:"networks"`
	// SMBNTHash is MD4(UTF16LE(smb password)), hex-encoded, or empty when the
	// account has not enrolled an SMB credential — in which case it cannot
	// mount an object-storage share at all.
	//
	// A second credential, and necessarily so: NTLMv2 is computed under this
	// value, and it cannot be derived from PasswordHash. bcrypt and MD4 are
	// different one-way functions over the same input, with no conversion in
	// either direction, which is the same reason Samba keeps its own password
	// database.
	//
	// Never serialised. It is unsalted MD4 with no work factor — weaker at
	// rest than the bcrypt hash beside it, and password-equivalent for SMB, so
	// it is treated like PasswordHash rather than like a hash.
	SMBNTHash string `json:"-"`
	// SMBEnrolled reports whether SMBNTHash is set, so the UI can show an
	// account's enrolment state without the hash itself ever reaching a
	// response body. Derived on read, never stored.
	SMBEnrolled bool      `json:"smb_enrolled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// HasSMBCredential reports whether the account may authenticate to the SMB
// view. Exposed so the UI can show enrolment state without the hash itself
// ever reaching a response body.
func (a Account) HasSMBCredential() bool { return a.SMBNTHash != "" }

// UpdateFields holds optional fields for updating an account. Only non-nil
// pointer fields are applied during an update. Password must be at least
// 8 characters; Email must match a standard email pattern; Phone must
// contain digits with optional formatting characters.
type UpdateFields struct {
	Password *string `json:"password,omitempty"` //nolint:gosec // G117 -- request field, not hardcoded
	Email    *string `json:"email,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	RealName *string `json:"real_name,omitempty"`
	Admin    *bool   `json:"admin,omitempty"`
	// WireGuard toggles the WireGuard-only restriction. Unlike Admin (which is
	// immutable after creation), an operator may turn this on or off on an
	// existing account. When it is turned on, Networks must resolve to a
	// non-empty set (either supplied here or already stored).
	WireGuard *bool `json:"wireguard,omitempty"`
	// Networks replaces the account's network scope. A nil pointer leaves the
	// stored scope untouched; a non-nil pointer replaces it wholesale.
	Networks *[]string `json:"networks,omitempty"`
	// SMBPassword enrols or replaces the account's SMB credential. The
	// plaintext is hashed to MD4(UTF16LE(...)) on the way in and never stored,
	// exactly like Password.
	//
	// A pointer so three states are distinguishable: nil leaves the stored
	// credential alone, a non-empty string sets one, and the empty string
	// withdraws it — after which the account can no longer mount a share. Two
	// states would make "no change" and "revoke" the same request.
	SMBPassword *string `json:"smb_password,omitempty"` //nolint:gosec // G117 -- request field, not a hardcoded credential
}

// Manager defines the interface for account CRUD and authentication operations.
type Manager interface {
	// Create creates a new user account. The password must be at least
	// 8 characters. Email, phone, and realName are all required. When admin
	// is true the account receives administrator privileges.
	Create(username, password, email, phone, realName string, admin bool) (*Account, error)
	// CreateWireGuard creates a WireGuard-only account scoped to networks.
	//
	// It is a separate method rather than a flag on Create so the dozens of
	// existing Create call sites are untouched, and so the security-relevant
	// invariant — a WireGuard account is never an admin, and always has a
	// non-empty network scope — is enforced in one place at creation time
	// rather than assembled from a widened positional signature. networks must
	// be non-empty; each entry must be a valid network name.
	CreateWireGuard(username, password, email, phone, realName string, networks []string) (*Account, error)
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
	if fields.Networks != nil {
		// Only the *names* are validated here, not the non-emptiness. Clearing
		// the scope to empty is legal on an update — e.g. turning WireGuard off
		// in the same call. Whether the *resulting* account may have an empty
		// scope is decided by validateWireGuardResult (SQLite) / the mock's
		// resolution, which know the post-update WireGuard state; enforcing
		// non-emptiness here would reject that legitimate case.
		if err := validateNetworkNames(*fields.Networks); err != nil {
			return err
		}
	}
	return nil
}

// validateNetworkScope checks that a WireGuard account's network list is
// non-empty and every entry is a legal network name. It is deliberately strict:
// a malformed or empty scope on a WireGuard account is a fail-open risk, so it
// is rejected at the boundary rather than stored and interpreted later. Used on
// the create path, where the account is WireGuard by construction.
func validateNetworkScope(networks []string) error {
	if len(networks) == 0 {
		return ErrWireGuardNoNetworks
	}
	return validateNetworkNames(networks)
}

// validateNetworkNames checks that every entry is a legal network name, without
// any opinion on whether the list may be empty. The emptiness rule is
// context-dependent (only a WireGuard account must be non-empty), so it lives
// with the code that knows that context.
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
