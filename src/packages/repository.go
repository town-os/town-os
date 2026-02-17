package packages

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

const RepositoriesFile = "repositories.json"

type RepositoryMap map[string]Repository

type RepositoryRoot struct {
	BaseDir string
	Items   RepositoryMap
}

func RepositoryRootFromBase(baseDir string) (*RepositoryRoot, error) {
	fn := filepath.Join(baseDir, RepositoriesFile)
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	items := RepositoryMap{}
	de := json.NewDecoder(f)
	if err := de.Decode(&items); err != nil {
		return nil, err
	}

	return &RepositoryRoot{
		BaseDir: baseDir,
		Items:   items,
	}, nil
}

func (rr *RepositoryRoot) save() error {
	fn := filepath.Join(rr.BaseDir, RepositoriesFile)
	f, err := os.Create(fn)
	if err != nil {
		return err
	}

	defer f.Close()

	en := json.NewEncoder(f)
	en.SetIndent("", "  ")
	return en.Encode(rr.Items)
}

func (rr *RepositoryRoot) Add(key string, repo Repository) error {
	if _, exists := rr.Items[key]; exists {
		return fmt.Errorf("repository %s already exists", key)
	}

	rr.Items[key] = repo
	return rr.save()
}

func (rr *RepositoryRoot) Remove(key string) error {
	if _, exists := rr.Items[key]; !exists {
		return fmt.Errorf("repository %s not found", key)
	}

	delete(rr.Items, key)
	return rr.save()
}

type Repository struct {
	Name string
	URL  url.URL
}

func runGit(baseDir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = baseDir
	return cmd.Run()
}

func NewRepository(baseDir string, u url.URL) (*Repository, error) {
	name := strings.TrimSuffix(path.Base(u.Path), ".git")
	r := &Repository{Name: name, URL: u}
	return r, r.init(baseDir)
}

func (r *Repository) init(baseDir string) error {
	target := filepath.Join(baseDir, r.Name)

	s, err := os.Stat(target)
	if os.IsNotExist(err) {
		if err := runGit(baseDir, "clone", r.URL.String(), r.Name); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !s.IsDir() {
		return fmt.Errorf("sub-path %s is not a directory", target)
	} else {
		if err := runGit(target, "stash"); err != nil {
			return err
		}

		if err := runGit(target, "pull", "--rebase"); err != nil {
			return err
		}

		if err := runGit(target, "stash", "apply"); err != nil {
			return err
		}
	}

	return nil
}
