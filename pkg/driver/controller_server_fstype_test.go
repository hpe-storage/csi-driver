// Copyright 2026 Hewlett Packard Enterprise Development LP
package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRequestedFsTypeConflictsWithParent validates the ESC-18117 fix: when restoring a
// PVC from a VolumeSnapshot, the fsType-parity guard must only fire between two filesystem
// (Mount) volumes. A raw Block target has an empty requested filesystem and must never be
// rejected, so a Filesystem->Block volumeMode conversion (KEP-3141) is allowed.
func TestRequestedFsTypeConflictsWithParent(t *testing.T) {
	tests := []struct {
		name                string
		parentVolFsType     string
		requestedFilesystem string
		wantConflict        bool
	}{
		{
			name:                "ESC-18117: filesystem parent, block target (empty fs) is allowed",
			parentVolFsType:     "xfs",
			requestedFilesystem: "",
			wantConflict:        false,
		},
		{
			name:                "block parent, block target is allowed",
			parentVolFsType:     "",
			requestedFilesystem: "",
			wantConflict:        false,
		},
		{
			name:                "block parent, filesystem target is allowed",
			parentVolFsType:     "",
			requestedFilesystem: "xfs",
			wantConflict:        false,
		},
		{
			name:                "same filesystem on both is allowed",
			parentVolFsType:     "xfs",
			requestedFilesystem: "xfs",
			wantConflict:        false,
		},
		{
			name:                "different filesystem is a conflict",
			parentVolFsType:     "xfs",
			requestedFilesystem: "ext4",
			wantConflict:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestedFsTypeConflictsWithParent(tt.parentVolFsType, tt.requestedFilesystem)
			assert.Equal(t, tt.wantConflict, got)
		})
	}
}
