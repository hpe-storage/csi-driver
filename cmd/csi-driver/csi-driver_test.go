// Copyright 2026 Hewlett Packard Enterprise Development LP
package main

import (
	"fmt"
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

func TestLoggerHooksDoNotAccumulate(t *testing.T) {
	initialHookCount := len(logrus.StandardLogger().Hooks[logrus.InfoLevel])

	withCapturedLogs(func(hook *testLogHook) {
		logStartupBanner()
	})

	afterHookCount := len(logrus.StandardLogger().Hooks[logrus.InfoLevel])
	if initialHookCount != afterHookCount {
		t.Fatalf("expected hook count to remain %d, but got %d", initialHookCount, afterHookCount)
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

func TestGetStartupBanner(t *testing.T) {
	testVersion := "3.4.0-alpha1"
	banner := getStartupBanner(testVersion)
	if len(banner) != 3 {
		t.Fatalf("expected 3 banner lines, got %d", len(banner))
	}
	expectedMsg := fmt.Sprintf("******** HPE CSI DRIVER Version: %s ********", testVersion)
	if banner[1] != expectedMsg {
		t.Errorf("expected banner line 1 to be %q, got %q", expectedMsg, banner[1])
	}
}

func TestLogStartupBannerDefaultVersion(t *testing.T) {
	origVersion := driverVersion
	defer func() { driverVersion = origVersion }()

	driverVersion = "3.4.0-alpha1"
	withCapturedLogs(func(hook *testLogHook) {
		logStartupBanner()

		expectedMsg := fmt.Sprintf("******** HPE CSI DRIVER Version: %s ********", driverVersion)
		found := false
		for _, msg := range hook.messages {
			if strings.Contains(msg, expectedMsg) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected startup banner log containing %q, got %v", expectedMsg, hook.messages)
		}
	})
}

func TestLogStartupBannerCustomVersion(t *testing.T) {
	origVersion := driverVersion
	defer func() { driverVersion = origVersion }()

	driverVersion = "9.9.9-test"
	withCapturedLogs(func(hook *testLogHook) {
		logStartupBanner()

		expectedMsg := "******** HPE CSI DRIVER Version: 9.9.9-test ********"
		found := false
		for _, msg := range hook.messages {
			if strings.Contains(msg, expectedMsg) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected startup banner log containing %q, got %v", expectedMsg, hook.messages)
		}
	})
}
