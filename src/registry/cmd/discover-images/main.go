// discover-images scans test package repositories and prints the docker.io
// container images they reference (one per line, deduplicated).
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// collectImages extracts all normalized container image URLs from a package
// table, deduplicating them.
func collectImages(pkgs packages.PackageTable) map[string]bool {
	seen := map[string]bool{}
	for _, versions := range pkgs {
		for _, pkg := range versions {
			if img := packages.NormalizeImageURL(pkg.Image); img != "" {
				seen[img] = true
			}
			for _, archive := range pkg.Archives {
				if img := packages.NormalizeImageURL(archive.Image); img != "" {
					seen[img] = true
				}
			}
		}
	}
	return seen
}

// filterDockerIO returns a sorted list of images that start with "docker.io/".
func filterDockerIO(seen map[string]bool) []string {
	var images []string
	for img := range seen {
		if strings.HasPrefix(img, "docker.io/") {
			images = append(images, img)
		}
	}
	sort.Strings(images)
	return images
}

func run() error {
	dir, err := os.MkdirTemp("", "discover-images-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck // best-effort cleanup

	username := os.Getenv(packages.EnvRepoUsername)
	password := os.Getenv(packages.EnvRepoPassword)

	g := &git.GoGitClient{Home: dir}
	allSeen := map[string]bool{}

	for _, repo := range packages.TestRepositories() {
		r, err := packages.NewRepository(dir, repo.Name, repo.URL, username, password, g)
		if err != nil {
			return fmt.Errorf("init repository %s: %w", repo.Name, err)
		}

		pkgs, err := r.LoadPackages(dir)
		if err != nil {
			return fmt.Errorf("load packages from %s: %w", repo.Name, err)
		}

		for img := range collectImages(pkgs) {
			allSeen[img] = true
		}
	}

	for _, img := range filterDockerIO(allSeen) {
		if _, err := os.Stdout.WriteString(img + "\n"); err != nil {
			return err
		}
	}

	return nil
}
