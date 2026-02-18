package account

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound           = errors.New("account not found")
	ErrDuplicateUsername   = errors.New("username already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrMissingContactInfo  = errors.New("email, phone, and real name are required")
)

type Account struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Email        string    `json:"email"`
	Phone        string    `json:"phone"`
	RealName     string    `json:"real_name"`
	Admin        bool      `json:"admin"`
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
	Delete(username string) error
	List() ([]Account, error)
	Authenticate(username, password string) (*Account, error)
}

func validateContactInfo(email, phone, realName string) error {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(phone) == "" || strings.TrimSpace(realName) == "" {
		return ErrMissingContactInfo
	}
	return nil
}
