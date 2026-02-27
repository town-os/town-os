package main

import (
	"sort"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestCollectImagesBasic(t *testing.T) {
	pkgs := packages.PackageTable{
		"nginx": {
			"1.0": packages.InputPackage{Image: "nginx:1.0"},
			"2.0": packages.InputPackage{Image: "nginx:2.0"},
		},
		"redis": {
			"7.0": packages.InputPackage{Image: "redis:7.0"},
		},
	}

	seen := collectImages(pkgs)

	// All images should be normalized.
	expected := []string{
		"docker.io/library/nginx:1.0",
		"docker.io/library/nginx:2.0",
		"docker.io/library/redis:7.0",
	}

	for _, img := range expected {
		if !seen[img] {
			t.Errorf("expected image %q to be collected", img)
		}
	}
}

func TestCollectImagesDeduplication(t *testing.T) {
	pkgs := packages.PackageTable{
		"nginx": {
			"1.0": packages.InputPackage{Image: "nginx:1.0"},
			"1.1": packages.InputPackage{Image: "nginx:1.0"}, // same image
		},
	}

	seen := collectImages(pkgs)

	if len(seen) != 1 {
		t.Fatalf("expected 1 unique image, got %d", len(seen))
	}
}

func TestCollectImagesWithArchives(t *testing.T) {
	pkgs := packages.PackageTable{
		"myapp": {
			"1.0": packages.InputPackage{
				Image: "myapp:1.0",
				Archives: []packages.InputPackageArchive{
					{Image: "backup-tool:latest"},
					{Image: "ghcr.io/org/archive-runner:v2"},
				},
			},
		},
	}

	seen := collectImages(pkgs)

	if !seen["docker.io/library/myapp:1.0"] {
		t.Error("expected myapp image")
	}
	if !seen["docker.io/library/backup-tool:latest"] {
		t.Error("expected backup-tool image")
	}
	if !seen["ghcr.io/org/archive-runner:v2"] {
		t.Error("expected archive-runner image")
	}
}

func TestCollectImagesEmptyImage(t *testing.T) {
	pkgs := packages.PackageTable{
		"pkg": {
			"1.0": packages.InputPackage{Image: ""},
		},
	}

	seen := collectImages(pkgs)

	if len(seen) != 0 {
		t.Fatalf("expected 0 images for empty image field, got %d", len(seen))
	}
}

func TestCollectImagesEmptyTable(t *testing.T) {
	pkgs := packages.PackageTable{}
	seen := collectImages(pkgs)

	if len(seen) != 0 {
		t.Fatalf("expected 0 images for empty table, got %d", len(seen))
	}
}

func TestFilterDockerIO(t *testing.T) {
	seen := map[string]bool{
		"docker.io/library/nginx:1.0":     true,
		"docker.io/library/redis:7.0":     true,
		"ghcr.io/org/myapp:v1":            true,
		"quay.io/team/worker:latest":      true,
		"docker.io/myuser/custom-app:2.0": true,
	}

	images := filterDockerIO(seen)

	expected := []string{
		"docker.io/library/nginx:1.0",
		"docker.io/library/redis:7.0",
		"docker.io/myuser/custom-app:2.0",
	}

	if len(images) != len(expected) {
		t.Fatalf("expected %d docker.io images, got %d: %v", len(expected), len(images), images)
	}

	// Should be sorted.
	if !sort.StringsAreSorted(images) {
		t.Fatalf("expected sorted output, got %v", images)
	}

	for i, img := range images {
		if img != expected[i] {
			t.Errorf("expected images[%d] = %q, got %q", i, expected[i], img)
		}
	}
}

func TestFilterDockerIOEmpty(t *testing.T) {
	seen := map[string]bool{
		"ghcr.io/org/myapp:v1": true,
	}

	images := filterDockerIO(seen)

	if len(images) != 0 {
		t.Fatalf("expected 0 docker.io images, got %d", len(images))
	}
}

func TestFilterDockerIOEmptyInput(t *testing.T) {
	images := filterDockerIO(map[string]bool{})

	if len(images) != 0 {
		t.Fatalf("expected 0 images, got %d", len(images))
	}
}
