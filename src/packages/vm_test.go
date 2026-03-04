package packages

import (
	"errors"
	"testing"
)

func TestVMPackageCompile(t *testing.T) {
	t.Run("basic VM package compiles", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{External: map[string]string{"8022": "22"}},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			VM:          &InputPackageVM{Image: "https://example.com/debian.qcow2", Memory: "2gb", CPUs: 2},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Runtime != RuntimeVM {
			t.Fatalf("expected RuntimeVM, got %s", p.Runtime)
		}
		if p.VM == nil {
			t.Fatal("expected VM config to be non-nil")
		}
		if p.VM.Image != "https://example.com/debian.qcow2" {
			t.Fatalf("expected VM image URL, got %s", p.VM.Image)
		}
		if p.VM.Memory != 2147483648 {
			t.Fatalf("expected 2GB in bytes, got %d", p.VM.Memory)
		}
		if p.VM.CPUs != 2 {
			t.Fatalf("expected 2 CPUs, got %d", p.VM.CPUs)
		}
		if p.Image != "" {
			t.Fatalf("expected empty Image for VM package, got %s", p.Image)
		}
	})

	t.Run("VM defaults memory and cpus", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			VM:          &InputPackageVM{Image: "debian.raw"},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.VM.Memory != 1073741824 {
			t.Fatalf("expected 1GB default memory, got %d", p.VM.Memory)
		}
		if p.VM.CPUs != 1 {
			t.Fatalf("expected 1 default CPU, got %d", p.VM.CPUs)
		}
	})

	t.Run("VM image template substitution", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"imgurl": {Query: "VM image URL?"}},
			VM:          &InputPackageVM{Image: "@imgurl@"},
		}
		p, err := input.Compile(Responses{"imgurl": "https://example.com/custom.qcow2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.VM.Image != "https://example.com/custom.qcow2" {
			t.Fatalf("expected templated VM image, got %s", p.VM.Image)
		}
	})

	t.Run("VM memory template substitution", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{"ram": {Query: "How much RAM?", Type: Bytes}},
			VM:          &InputPackageVM{Image: "debian.raw", Memory: "@ram@"},
		}
		p, err := input.Compile(Responses{"ram": "4gb"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Bytes type normalizes "4gb" to "4294967296" during template substitution.
		if p.VM.Memory != 4294967296 {
			t.Fatalf("expected 4GB in bytes, got %d", p.VM.Memory)
		}
	})

	t.Run("mixed runtime rejected", func(t *testing.T) {
		input := InputPackage{
			Image:       InputPackageImage{URL: "debian:latest"},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			VM:          &InputPackageVM{Image: "debian.raw"},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for mixed runtime")
		}
		if !errors.Is(err, ErrMixedRuntime) {
			t.Fatalf("expected ErrMixedRuntime, got %v", err)
		}
	})

	t.Run("no runtime rejected", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for no runtime")
		}
		if !errors.Is(err, ErrNoRuntime) {
			t.Fatalf("expected ErrNoRuntime, got %v", err)
		}
	})

	t.Run("VM with empty image rejected", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			VM:          &InputPackageVM{Image: ""},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for empty VM image")
		}
		if !errors.Is(err, ErrInvalidVMConfig) {
			t.Fatalf("expected ErrInvalidVMConfig, got %v", err)
		}
	})

	t.Run("VM with negative CPUs rejected", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			VM:          &InputPackageVM{Image: "debian.raw", CPUs: -1},
		}
		_, err := input.Compile(Responses{})
		if err == nil {
			t.Fatal("expected error for negative CPUs")
		}
		if !errors.Is(err, ErrInvalidVMConfig) {
			t.Fatalf("expected ErrInvalidVMConfig, got %v", err)
		}
	})

	t.Run("container package sets RuntimeContainer", func(t *testing.T) {
		input := InputPackage{
			Image:       InputPackageImage{URL: "debian:latest"},
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Runtime != RuntimeContainer {
			t.Fatalf("expected RuntimeContainer, got %s", p.Runtime)
		}
		if p.VM != nil {
			t.Fatal("expected VM to be nil for container package")
		}
	})

	t.Run("VM package does not normalize image URL", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{},
			Volumes:     map[string]InputPackageVolume{},
			Questions:   map[string]Question{},
			VM:          &InputPackageVM{Image: "debian.raw"},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Image field should remain empty (not normalized as a container image).
		if p.Image != "" {
			t.Fatalf("expected empty image for VM package, got %s", p.Image)
		}
	})

	t.Run("VM with volumes compiles", func(t *testing.T) {
		input := InputPackage{
			Environment: map[string]string{},
			Network:     InputPackageNetwork{External: map[string]string{"8022": "22"}},
			Volumes:     map[string]InputPackageVolume{"data": {Mountpoint: "/data", Quota: "10gb"}},
			Questions:   map[string]Question{},
			VM:          &InputPackageVM{Image: "debian.raw", Memory: "512mb", CPUs: 1},
		}
		p, err := input.Compile(Responses{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.VM.Memory != 536870912 {
			t.Fatalf("expected 512MB in bytes, got %d", p.VM.Memory)
		}
		if len(p.Volumes) != 1 {
			t.Fatalf("expected 1 volume, got %d", len(p.Volumes))
		}
		if p.Volumes["data"].Quota != 10737418240 {
			t.Fatalf("expected 10GB quota, got %d", p.Volumes["data"].Quota)
		}
	})
}

func TestRuntimeType(t *testing.T) {
	t.Run("container package", func(t *testing.T) {
		ip := InputPackage{Image: InputPackageImage{URL: "nginx:latest"}}
		if ip.RuntimeType() != RuntimeContainer {
			t.Fatalf("expected RuntimeContainer, got %s", ip.RuntimeType())
		}
	})

	t.Run("VM package", func(t *testing.T) {
		ip := InputPackage{VM: &InputPackageVM{Image: "debian.raw"}}
		if ip.RuntimeType() != RuntimeVM {
			t.Fatalf("expected RuntimeVM, got %s", ip.RuntimeType())
		}
	})
}
