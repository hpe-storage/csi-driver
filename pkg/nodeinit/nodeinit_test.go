// Copyright 2026 Hewlett Packard Enterprise Development LP
package nodeinit

import "testing"

func TestNewNodeInitContainerReturnsInstance(t *testing.T) {
	nic := NewNodeInitContainer("vanilla")
	if nic == nil {
		t.Fatalf("expected non-nil NodeInitContainer")
	}
}
