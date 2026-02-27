// Package account defines user account types, validation, and the [Manager]
// interface used by the Control Plane Service.
package account

import (
	"errors"
	"regexp"
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
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
}

// Manager defines the interface for account CRUD and authentication operations.
type Manager interface {
	// Create creates a new user account. The password must be at least
	// 8 characters. Email, phone, and realName are all required. When admin
	// is true the account receives administrator privileges.
	Create(username, password, email, phone, realName string, admin bool) (*Account, error)
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
	return nil
}
