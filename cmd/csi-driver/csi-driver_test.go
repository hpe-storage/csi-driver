// Copyright 2026 Hewlett Packard Enterprise Development LP
package main

import (
	"strings"
	"testing"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/sirupsen/logrus"
)

type testLogHook struct {
	messages []string
}

func (h *testLogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *testLogHook) Fire(entry *logrus.Entry) error {
	h.messages = append(h.messages, entry.Message)
	return nil
}

// withCapturedLogs installs a temporary hook on the standard logger and guarantees
// that previous hooks are restored when fn completes, preventing hook accumulation across tests.
func withCapturedLogs(fn func(hook *testLogHook)) {
	hook := &testLogHook{}
	oldHooks := logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	defer logrus.StandardLogger().ReplaceHooks(oldHooks)

	log.AddHook(hook)
	fn(hook)
}

func TestRootCmdIsInitialized(t *testing.T) {
	if RootCmd == nil {
		t.Fatalf("RootCmd must be initialized")
	}
	if RootCmd.Version != getDriverVersion() {
		t.Errorf("expected RootCmd.Version %q, got %q", getDriverVersion(), RootCmd.Version)
	}
}

func TestCSIVersionRemainsInterfaceVersion(t *testing.T) {
	if csiVersion != "1.3" {
		t.Fatalf("expected CSI specification version to remain %q, got %q", "1.3", csiVersion)
	}
	if csiVersion == getDriverVersion() {
		t.Fatalf("CSI interface version (%q) and driver product version (%q) must remain separate", csiVersion, getDriverVersion())
	}
}

func TestGetDriverVersion(t *testing.T) {
	origVersion := driverVersion
	defer func() { driverVersion = origVersion }()

	driverVersion = "3.4.0-alpha1"
	if v := getDriverVersion(); v != "3.4.0-alpha1" {
		t.Errorf("expected 3.4.0-alpha1, got %s", v)
	}

	driverVersion = "3.4.0-custom"
	if v := getDriverVersion(); v != "3.4.0-custom" {
		t.Errorf("expected 3.4.0-custom, got %s", v)
	}

	driverVersion = ""
	if v := getDriverVersion(); v != "3.4.0-alpha1" {
		t.Errorf("expected fallback 3.4.0-alpha1, got %s", v)
	}
}

func TestLogStartupBanner(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		expectedMsg string
	}{
		{
			name:        "default version",
			version:     "3.4.0-alpha1",
			expectedMsg: "******** HPE CSI DRIVER Version: 3.4.0-alpha1 ********",
		},
		{
			name:        "custom version",
			version:     "9.9.9-test",
			expectedMsg: "******** HPE CSI DRIVER Version: 9.9.9-test ********",
		},
	}

	origVersion := driverVersion
	defer func() { driverVersion = origVersion }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driverVersion = tt.version
			withCapturedLogs(func(hook *testLogHook) {
				logStartupBanner()

				found := false
				for _, msg := range hook.messages {
					if strings.Contains(msg, tt.expectedMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected startup banner log containing %q, got %v", tt.expectedMsg, hook.messages)
				}
			})
		})
	}
}
