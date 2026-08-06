// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package main

import (
	"slices"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
)

// The boot pull set.
//
// This list is not a performance detail. A unit whose image is not already local
// pulls it from inside `podman run`, so everything the controller does next —
// above all the readiness waits — is racing a registry download. gfeh was the
// single system-service image missing from it, which is why its partitions were
// reliably not up by the time boot finished asking whether they were.

func TestCoreBootImagesIncludesGfeh(t *testing.T) {
	images := coreBootImages("nc:test", "ui:test", "gfeh:test", monitoring.BackendUPlot)

	if !slices.Contains(images, "gfeh:test") {
		t.Fatalf("the object-storage image is not pulled at boot, so its unit pulls it "+
			"itself while the socket wait times out. images = %v", images)
	}
}

// Every system service that will actually run is pulled. Written as one
// assertion over the whole set so adding a service and forgetting the pull is a
// failing test rather than a slow boot nobody traces back here.
func TestCoreBootImagesCoversEveryEnabledService(t *testing.T) {
	images := coreBootImages("nc:test", "ui:test", "gfeh:test", monitoring.BackendGrafana)

	for _, want := range []string{
		"nc:test",
		"ui:test",
		"gfeh:test",
		monitoring.PrometheusImage,
		monitoring.NodeExporterImage,
		monitoring.GrafanaImage,
	} {
		if !slices.Contains(images, want) {
			t.Errorf("%s is not in the boot pull set: %v", want, images)
		}
	}
}

// An empty image is the explicit off switch (GFEH_IMAGE="" / UI_IMAGE="", the
// dev-mode convention). Pulling "" would fail every boot with a confusing
// podman error for a service that is not going to run.
func TestCoreBootImagesSkipsDisabledServices(t *testing.T) {
	images := coreBootImages("nc:test", "", "", monitoring.BackendUPlot)

	if slices.Contains(images, "") {
		t.Errorf("an empty image reached the pull set: %v", images)
	}
	if len(images) != 3 {
		t.Errorf("images = %v, want only the NC and the two always-on monitoring images", images)
	}
}

// Grafana is ~771 MB and only one of the two backends. Pulling it under uplot
// would cost every box that never opens Grafana most of a gigabyte.
func TestCoreBootImagesSkipsGrafanaUnderUPlot(t *testing.T) {
	images := coreBootImages("nc:test", "ui:test", "gfeh:test", monitoring.BackendUPlot)

	if slices.Contains(images, monitoring.GrafanaImage) {
		t.Errorf("Grafana was pulled under the uplot backend: %v", images)
	}
}
