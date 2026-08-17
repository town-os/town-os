package packages

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// OAuthQuestionNames returns the names of the questions answered by running a
// device flow. It is the set whose answers are vendor credentials rather than
// operator preferences, which is what makes them the ones a purge has to drop.
func OAuthQuestionNames(questions map[string]Question) []string {
	names := make([]string, 0, len(questions))
	for name, q := range questions {
		if q.Type == Oauth {
			names = append(names, name)
		}
	}
	return names
}

// ForgetResponseKeys removes the named answers from every stored response for a
// package -- each version's file under responses/<repo>/<pkg>/ and the
// package-wide responses/last/<repo>/<pkg>.json -- leaving every other answer
// exactly as it was.
//
// This is what makes a purge actually purge. Volumes are only half of a
// package's state: the answers behind them outlive an uninstall deliberately, so
// that reinstalling does not re-interrogate the operator about storage sizes and
// ports. For a credential minted against the instance the purge just destroyed,
// that carry-forward is precisely wrong. plex's account token was exchanged for
// a claim bound to a server identity that lived in the config volume; purge the
// volume and the next start generates a NEW MachineIdentifier, so re-using the
// old answer claims a new server with a stale association and lands unclaimed --
// the operator sees "not authorized" and has no way to ask for the flow again,
// because the install dialog helpfully pre-filled the answer they wanted to
// replace.
//
// A missing file is not an error: a package with no stored responses has nothing
// to forget, which is the normal case for everything that has no oauth question.
func (m *InstallManager) ForgetResponseKeys(repoName, pkgName string, keys []string) (err error) {
	if len(keys) == 0 {
		return nil
	}

	lock, err := lockDir(m.BaseDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	drop := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		drop[k] = struct{}{}
	}

	paths, err := m.responseFilesForPackage(repoName, pkgName)
	if err != nil {
		return err
	}

	for _, path := range paths {
		if err := forgetKeysInResponseFile(path, drop); err != nil {
			return err
		}
	}

	return nil
}

// responseFilesForPackage lists every stored response file for a package: the
// package-wide last-responses file plus one per installed version.
func (m *InstallManager) responseFilesForPackage(repoName, pkgName string) ([]string, error) {
	paths := []string{m.lastResponsesPath(repoName, pkgName)}

	dir := m.responsesPkgDir(repoName, pkgName)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		// A subdirectory is a parent's `subpackages/` tree. A dependency's own
		// answers belong to the dependency and are forgotten when it is purged,
		// not when its parent is.
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}

	return paths, nil
}

// forgetKeysInResponseFile rewrites one response file without the dropped keys,
// and leaves it untouched when it carries none of them.
func forgetKeysInResponseFile(path string, drop map[string]struct{}) error {
	f, err := os.Open(path) //nolint:gosec // G304 -- path from responsesPkgDir/lastResponsesPath
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var resp Responses
	decodeErr := json.NewDecoder(f).Decode(&resp)
	if closeErr := f.Close(); closeErr != nil {
		return errors.Join(decodeErr, closeErr)
	}
	if decodeErr != nil {
		return decodeErr
	}

	removed := false
	for key := range drop {
		if _, ok := resp[key]; ok {
			delete(resp, key)
			removed = true
		}
	}
	if !removed {
		return nil
	}

	return atomicWriteJSON(path, resp)
}
