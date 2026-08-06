// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Config is gfehd's configuration file.
//
// gfehd parses this with serde(deny_unknown_fields), so a key it does not know
// is a hard startup failure rather than a warning — which means this struct has
// to stay field-for-field with crates/gfehd/src/config.rs. That strictness is
// deliberate on their side (a typo in a listener address that silently leaves a
// view unserved is a deployment that looks healthy and answers nothing), and it
// makes drift here loud instead of subtle.
//
// Every optional section is a pointer with omitempty: a view with no bind
// address is a view that is not served, and that absence is how a deployment
// turns one off. Emitting `s3: null` would not be the same thing.
type Config struct {
	// DataDir is where partition directories live; the partition's own
	// directory is DataDir/Partition.
	DataDir string `yaml:"data_dir"`
	// Partition is the single partition this daemon serves. One per daemon:
	// a partition is a subvolume with its own quota, index, and change
	// sequence, and serving several from one process would mean a change feed
	// whose page tokens mean different things depending on which moved last.
	Partition string `yaml:"partition"`
	// Network is the Town OS network the partition is bound to. It decides
	// which zone the partition's names are published under and whether they
	// are LAN-visible or scoped to an overlay — but that decision is Town OS's,
	// and this field only says which network it belongs to.
	//
	// A pointer, and omitempty, because absent and empty are different
	// requests: absent means "the default", and an empty string would be a
	// request to publish under a zone called "".
	Network *string `yaml:"network,omitempty"`
	// AdminSocket is the Unix socket the administrative surface binds to.
	AdminSocket string `yaml:"admin_socket"`

	S3    *S3Config    `yaml:"s3,omitempty"`
	HTTP  *HTTPConfig  `yaml:"http,omitempty"`
	Drive *DriveConfig `yaml:"drive,omitempty"`
	IPFS  *IPFSConfig  `yaml:"ipfs,omitempty"`
	SMB   *SMBConfig   `yaml:"smb,omitempty"`

	Credentials []CredentialConfig `yaml:"credentials,omitempty"`

	// No town_os field. That section named a Town OS account for gfehd to
	// authenticate to the control plane as, and there is no such account: the
	// partition's subvolume and quota are provisioned from the Town OS side
	// before the daemon starts, and its principals are created over the admin
	// socket. gfehd's schema still accepts the key from a standalone
	// deployment; Town OS has no way to produce it.
}

// S3Config is the S3 listener.
type S3Config struct {
	Bind   string `yaml:"bind"`
	Region string `yaml:"region,omitempty"`
	// Hostname is the label Town OS should publish this view under. Left unset
	// gfehd derives "<view>.<partition>", which is what we want — the label is
	// gfeh's to choose and the zone is ours.
	Hostname string `yaml:"hostname,omitempty"`
}

// HTTPConfig is the plain-HTTP listener that serves published links.
type HTTPConfig struct {
	Bind     string `yaml:"bind"`
	Hostname string `yaml:"hostname,omitempty"`
}

// DriveConfig is the Google Drive listener.
type DriveConfig struct {
	Bind     string        `yaml:"bind"`
	Tokens   []TokenConfig `yaml:"tokens,omitempty"`
	Hostname string        `yaml:"hostname,omitempty"`
}

// IPFSConfig is the IPFS gateway and API listener.
type IPFSConfig struct {
	Bind     string `yaml:"bind"`
	Hostname string `yaml:"hostname,omitempty"`
}

// SMBConfig is the SMB listener.
type SMBConfig struct {
	Bind  string `yaml:"bind"`
	Share string `yaml:"share,omitempty"`
	// Principal is what a session acts as when Users is empty. Required by
	// gfehd either way, so a config that later drops its credential table
	// still has something to fall back to.
	Principal string `yaml:"principal"`
	// Users is the credential table. Non-empty switches gfehd into verifying
	// NTLMv2: a client must prove possession of the account's NT hash, and the
	// session runs as that account's principal rather than Principal.
	//
	// Empty is not a neutral default — it means anyone who can open a TCP
	// connection to the port gets whatever Principal was granted.
	Users    []SmbUserConfig `yaml:"users,omitempty"`
	Hostname string          `yaml:"hostname,omitempty"`
}

// SmbUserConfig is one account that may authenticate to the SMB view.
type SmbUserConfig struct {
	// Username is matched case-insensitively, as SMB account names are.
	Username string `yaml:"username"`
	// NTHash is MD4(UTF16LE(password)), hex-encoded: exactly 32 characters.
	//
	// A second credential, and necessarily so: NTLMv2 is computed under this
	// value and it cannot be derived from the bcrypt hash Town OS stores for
	// an account password. The two are different one-way functions over the
	// same input, so there is no conversion in either direction.
	//
	// It is password-equivalent for SMB and weaker at rest than the bcrypt
	// hash beside it — unsalted MD4, no work factor. Treat it as a password.
	NTHash string `yaml:"nt_hash"`
	// Principal is what a session for this account acts as.
	Principal string `yaml:"principal"`
}

// TokenConfig maps a Drive bearer token to the principal it acts as.
type TokenConfig struct {
	Token     string `yaml:"token"`
	Principal string `yaml:"principal"`
}

// CredentialConfig maps an S3 access key to the principal it acts as.
type CredentialConfig struct {
	AccessKey string `yaml:"access_key"`
	Secret    string `yaml:"secret"`
	Principal string `yaml:"principal"`
}

// Validate applies the checks gfehd itself applies, so a bad render is caught
// where the fields have names rather than as a container that exits non-zero
// with a serde message.
func (c Config) Validate() error {
	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("gfeh config: data_dir is required")
	}
	if strings.TrimSpace(c.AdminSocket) == "" {
		return errors.New("gfeh config: admin_socket is required")
	}
	if strings.TrimSpace(c.Partition) == "" {
		return errors.New("gfeh config: partition is required")
	}
	if strings.Contains(c.Partition, "/") {
		return fmt.Errorf("gfeh config: partition %q cannot contain a separator", c.Partition)
	}
	if c.Network != nil && strings.TrimSpace(*c.Network) == "" {
		// gfehd would accept this and then ask Town OS to publish under a zone
		// called "". Absent is how "the default" is spelled.
		return errors.New("gfeh config: network is set but empty; omit it to mean the default network")
	}
	if c.SMB != nil {
		if strings.TrimSpace(c.SMB.Principal) == "" {
			return errors.New("gfeh config: the smb listener names no principal")
		}
		if err := validateSMBUsers(c.SMB.Users); err != nil {
			return err
		}
	}
	for _, cred := range c.Credentials {
		if strings.TrimSpace(cred.Principal) == "" {
			return fmt.Errorf("gfeh config: the access key %s names no principal", cred.AccessKey)
		}
		if cred.Secret == "" {
			return fmt.Errorf("gfeh config: the access key %s has no secret", cred.AccessKey)
		}
	}
	if c.Drive != nil {
		for _, tok := range c.Drive.Tokens {
			if strings.TrimSpace(tok.Principal) == "" || tok.Token == "" {
				return errors.New("gfeh config: a drive token names no principal, or has no value")
			}
		}
	}
	return nil
}

// NTHashHexLen is the length of a hex-encoded NT hash: 16 bytes.
const NTHashHexLen = 32

// validateSMBUsers applies the same checks gfehd applies at load.
//
// Caught here so a bad credential is a reconcile error naming the account,
// rather than a container that exits with a serde message — or worse, a
// daemon that starts and refuses one specific person's password.
func validateSMBUsers(users []SmbUserConfig) error {
	seen := make(map[string]bool, len(users))
	for _, u := range users {
		name := strings.TrimSpace(u.Username)
		if name == "" {
			return errors.New("gfeh config: an smb user has no username")
		}
		if strings.TrimSpace(u.Principal) == "" {
			return fmt.Errorf("gfeh config: the smb user %s names no principal", name)
		}
		if !validNTHash(u.NTHash) {
			return fmt.Errorf("gfeh config: the smb user %s has an nt_hash that is not %d hex characters", name, NTHashHexLen)
		}
		// gfehd matches account names case-insensitively, so two spellings of
		// one name are two entries only one of which could ever be reached.
		lower := strings.ToLower(name)
		if seen[lower] {
			return fmt.Errorf("gfeh config: the smb user %s is listed twice", name)
		}
		seen[lower] = true
	}
	return nil
}

// validNTHash reports whether a string is exactly 16 hex-encoded bytes.
func validNTHash(hash string) bool {
	if len(hash) != NTHashHexLen {
		return false
	}
	for _, r := range hash {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// RenderConfig validates and marshals a config to YAML.
//
// The output is deterministic for a given input, which is what lets the
// reconcile path compare the rendered bytes against what is on disk and skip
// restarting a daemon whose configuration has not actually changed.
func RenderConfig(c Config) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("render gfeh config: %w", err)
	}
	return out, nil
}
