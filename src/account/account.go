package account

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

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

type UpdateFields struct {
	Password *string `json:"password,omitempty"`
	Email    *string `json:"email,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	RealName *string `json:"real_name,omitempty"`
	Admin    *bool   `json:"admin,omitempty"`
}

type Manager interface {
	Create(username, password, email, phone, realName string, admin bool) (*Account, error)
	Get(username string) (*Account, error)
	Update(username string, fields UpdateFields) (*Account, error)
	Disable(username string) error
	Enable(username string) error
	List() ([]Account, error)
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
	if err := validateEmail(email); err != nil {
		return err
	}
	if err := validatePhone(phone); err != nil {
		return err
	}
	return nil
}

func validateUpdateFields(fields UpdateFields) error {
	if fields.Password != nil {
		if err := validatePassword(*fields.Password); err != nil {
			return err
		}
	}
	if fields.Email != nil {
		if err := validateEmail(*fields.Email); err != nil {
			return err
		}
	}
	if fields.Phone != nil {
		if err := validatePhone(*fields.Phone); err != nil {
			return err
		}
	}
	return nil
}
