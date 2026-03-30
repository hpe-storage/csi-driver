// Copyright 2019 Hewlett Packard Enterprise Development LP
package driver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
	"github.com/stretchr/testify/assert"
)

type scrubberTestFlavor struct {
	*vanilla.Flavor
	podUIDs   []string
	podExists bool
}

func newScrubberTestFlavor(podExists bool) *scrubberTestFlavor {
	return &scrubberTestFlavor{Flavor: &vanilla.Flavor{}, podExists: podExists}
}

func (flavor *scrubberTestFlavor) IsPodExists(uid string) (bool, error) {
	flavor.podUIDs = append(flavor.podUIDs, uid)
	return flavor.podExists, nil
}

func TestScrubEphemeralPodsSkipsDeepFilesystemPaths(t *testing.T) {
	podsDirPath := t.TempDir()
	podUID := "pod-deep"

	ephemeralDataPath := filepath.Join(
		podsDirPath,
		podUID,
		"volumes",
		"kubernetes.io~csi",
		"pvc-123",
		"mount",
		"nested",
		ephemeralDataFileName,
	)
	writeTestEphemeralDataFile(t, ephemeralDataPath, podUID)

	testFlavor := newScrubberTestFlavor(false)
	driver := &Driver{flavor: testFlavor}

	err := driver.ScrubEphemeralPods(podsDirPath)
	assert.NoError(t, err)
	assert.Empty(t, testFlavor.podUIDs)
}

func TestScrubEphemeralPodsSkipsNonCSIPluginPaths(t *testing.T) {
	podsDirPath := t.TempDir()
	podUID := "pod-noncsi"

	ephemeralDataPath := filepath.Join(
		podsDirPath,
		podUID,
		"volumes",
		"kubernetes.io~foo",
		"pvc-123",
		ephemeralDataFileName,
	)
	writeTestEphemeralDataFile(t, ephemeralDataPath, podUID)

	testFlavor := newScrubberTestFlavor(false)
	driver := &Driver{flavor: testFlavor}

	err := driver.ScrubEphemeralPods(podsDirPath)
	assert.NoError(t, err)
	assert.Empty(t, testFlavor.podUIDs)
}

func TestScrubEphemeralPodsProcessesValidCSIPath(t *testing.T) {
	podsDirPath := t.TempDir()
	podUID := "pod-valid"

	ephemeralDataPath := filepath.Join(
		podsDirPath,
		podUID,
		"volumes",
		"kubernetes.io~csi",
		"pvc-123",
		ephemeralDataFileName,
	)
	writeTestEphemeralDataFile(t, ephemeralDataPath, podUID)

	testFlavor := newScrubberTestFlavor(true)
	driver := &Driver{flavor: testFlavor}

	err := driver.ScrubEphemeralPods(podsDirPath)
	assert.NoError(t, err)
	assert.Equal(t, []string{podUID}, testFlavor.podUIDs)
}

// TestScrubEphemeralPodsHybridPruning validates the hybrid approach:
// depth-based pruning + name-based pruning
// This ensures robustness against:
// 1. Deep filesystem walks into mounted content
// 2. Non-CSI plugin paths
// 3. Future Kubernetes path naming changes
func TestScrubEphemeralPodsHybridPruning(t *testing.T) {
	podsDirPath := t.TempDir()
	podUID := "pod-hybrid"

	// Valid path with correct structure
	validPath := filepath.Join(
		podsDirPath,
		podUID,
		"volumes",
		"kubernetes.io~csi",
		"pvc-xyz",
		ephemeralDataFileName,
	)
	writeTestEphemeralDataFile(t, validPath, podUID)

	// Create a deep path with ephemeral_data.json that should be skipped
	deepPath := filepath.Join(
		podsDirPath,
		podUID,
		"volumes",
		"kubernetes.io~csi",
		"pvc-xyz",
		"mount",
		"app-data",
		ephemeralDataFileName,
	)
	writeTestEphemeralDataFile(t, deepPath, "deep-pod-id")

	testFlavor := newScrubberTestFlavor(true)
	driver := &Driver{flavor: testFlavor}

	err := driver.ScrubEphemeralPods(podsDirPath)
	assert.NoError(t, err)
	// Should only find the valid pod, not the deep-nested one
	assert.Equal(t, []string{podUID}, testFlavor.podUIDs)
}

func writeTestEphemeralDataFile(t *testing.T, filePath string, podUID string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(filePath), 0o755)
	if !assert.NoError(t, err) {
		return
	}

	ephemeralData := &Ephemeral{
		VolumeID:     "volume-id",
		VolumeHandle: "volume-handle",
		PodData: &POD{
			UID: podUID,
		},
	}
	payload, err := json.Marshal(ephemeralData)
	if !assert.NoError(t, err) {
		return
	}

	err = os.WriteFile(filePath, payload, 0o644)
	assert.NoError(t, err)
}
