// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"testing"
)

func TestMockListVMImagesEmpty(t *testing.T) {
	m := InitMockClient()
	images, err := m.ListVMImages(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("expected empty slice, got %d items", len(images))
	}
	calls := m.GetCalls()
	if len(calls) != 1 || calls[0].Method != "ListVMImages" {
		t.Fatalf("expected ListVMImages call, got %v", calls)
	}
}

func TestMockListVMImagesWithData(t *testing.T) {
	m := InitMockClient()
	m.VMImages = []VMImageInfo{
		{Name: "debian.raw", Size: 1024},
		{Name: "alpine.raw", Size: 512},
	}
	images, err := m.ListVMImages(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Name != "debian.raw" {
		t.Fatalf("expected debian.raw, got %s", images[0].Name)
	}
}

func TestMockListVMImagesError(t *testing.T) {
	m := InitMockClient()
	m.ListVMImagesErr = errors.New("storage offline")
	_, err := m.ListVMImages(context.Background())
	if err == nil || err.Error() != "storage offline" {
		t.Fatalf("expected storage offline error, got %v", err)
	}
}

func TestMockUploadVMImageRecordsCall(t *testing.T) {
	m := InitMockClient()
	info, err := m.UploadVMImage(context.Background(), "https://example.com/debian.qcow2", "debian")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name != "debian" {
		t.Fatalf("expected name debian, got %s", info.Name)
	}
	calls := m.GetCalls()
	if len(calls) != 1 || calls[0].Method != "UploadVMImage" {
		t.Fatalf("expected UploadVMImage call, got %v", calls)
	}
	args := calls[0].Args
	if len(args) != 2 || args[0] != "https://example.com/debian.qcow2" || args[1] != "debian" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestMockUploadVMImageWithResult(t *testing.T) {
	m := InitMockClient()
	m.UploadVMImageResult = &VMImageInfo{Name: "custom.raw", Size: 2048}
	info, err := m.UploadVMImage(context.Background(), "https://example.com/custom.qcow2", "custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name != "custom.raw" || info.Size != 2048 {
		t.Fatalf("expected custom.raw with size 2048, got %s %d", info.Name, info.Size)
	}
}

func TestMockUploadVMImageError(t *testing.T) {
	m := InitMockClient()
	m.UploadVMImageErr = errors.New("conversion failed")
	_, err := m.UploadVMImage(context.Background(), "https://example.com/bad.img", "bad")
	if err == nil || err.Error() != "conversion failed" {
		t.Fatalf("expected conversion failed error, got %v", err)
	}
}

func TestMockDeleteVMImageRecordsCall(t *testing.T) {
	m := InitMockClient()
	err := m.DeleteVMImage(context.Background(), "debian.raw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := m.GetCalls()
	if len(calls) != 1 || calls[0].Method != "DeleteVMImage" {
		t.Fatalf("expected DeleteVMImage call, got %v", calls)
	}
	if calls[0].Args[0] != "debian.raw" {
		t.Fatalf("expected debian.raw arg, got %v", calls[0].Args[0])
	}
}

func TestMockDeleteVMImageError(t *testing.T) {
	m := InitMockClient()
	m.DeleteVMImageErr = errors.New("not found")
	err := m.DeleteVMImage(context.Background(), "missing.raw")
	if err == nil || err.Error() != "not found" {
		t.Fatalf("expected not found error, got %v", err)
	}
}
