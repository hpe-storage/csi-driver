// Copyright 2026 Hewlett Packard Enterprise Development LP
package main

import "testing"

func TestRootCmdIsInitialized(t *testing.T) {
	if RootCmd == nil {
		t.Fatalf("RootCmd must be initialized")
	}
}
