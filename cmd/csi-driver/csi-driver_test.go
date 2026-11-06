// Copyright 2026 Hewlett Packard Enterprise Development LP
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootCmdIsInitialized(t *testing.T) {
	if RootCmd == nil {
		t.Fatalf("RootCmd must be initialized")
	}
}

// ESC-18251: with DISABLE_NODE_CONFIGURATION=true the init container must not
// touch the host's systemd at all; configureNodeServices must return without
// exiting the process.
func TestConfigureNodeServicesSkippedWhenDisabled(t *testing.T) {
	t.Setenv("DISABLE_NODE_CONFIGURATION", "true")
	configureNodeServices()
}

// ESC-18251 parent D2 / fix verification F7: iscsid is configured only when the
// host shows iSCSI in use, unless an operator overrides the decision.
func TestShouldConfigureISCSI(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "iscsi_session")
	initiator := filepath.Join(root, "initiatorname.iscsi")
	probes := []string{sessions, initiator}

	t.Setenv(envDisableNodeISCSIConfig, "")
	t.Setenv(envForceNodeISCSIConfig, "")

	if got, _ := shouldConfigureISCSI(probes); got {
		t.Fatalf("FC-only host (no probe paths): expected false")
	}

	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := shouldConfigureISCSI(probes); got {
		t.Fatalf("empty session directory: expected false")
	}
	if err := os.WriteFile(filepath.Join(sessions, "session1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, reason := shouldConfigureISCSI(probes); !got {
		t.Fatalf("active session: expected true, reason %q", reason)
	}

	if err := os.RemoveAll(sessions); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initiator, []byte("InitiatorName=iqn.test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := shouldConfigureISCSI(probes); !got {
		t.Fatalf("initiator name file present: expected true")
	}

	t.Setenv(envDisableNodeISCSIConfig, "true")
	if got, _ := shouldConfigureISCSI(probes); got {
		t.Fatalf("opt-out must win over detection")
	}

	t.Setenv(envDisableNodeISCSIConfig, "")
	t.Setenv(envForceNodeISCSIConfig, "true")
	if got, _ := shouldConfigureISCSI([]string{filepath.Join(root, "absent")}); !got {
		t.Fatalf("force must win over a negative detection")
	}
}
