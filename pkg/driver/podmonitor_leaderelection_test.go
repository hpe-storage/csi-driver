// Copyright 2026 Hewlett Packard Enterprise Development LP

package driver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
	"github.com/hpe-storage/csi-driver/pkg/monitor"
)

func TestGetLeaderElectionDuration(t *testing.T) {
	const envVar = "TEST_PODMONITOR_LEASE_DURATION"

	t.Setenv(envVar, "")
	assert.Equal(t, 15*time.Second, getLeaderElectionDuration(envVar, 15*time.Second))

	t.Setenv(envVar, "30s")
	assert.Equal(t, 30*time.Second, getLeaderElectionDuration(envVar, 15*time.Second))

	t.Setenv(envVar, "not-a-duration")
	assert.Equal(t, 15*time.Second, getLeaderElectionDuration(envVar, 15*time.Second))
}

func TestNewPodMonitorLeaderElector(t *testing.T) {
	m := monitor.NewMonitor(&vanilla.Flavor{}, 30)
	clientset := fake.NewSimpleClientset()

	elector, err := newPodMonitorLeaderElector("hpe-storage", "test-pod", clientset, m)

	assert.NoError(t, err)
	assert.NotNil(t, elector)
}

func TestPodMonitorCallbacksStartStop(t *testing.T) {
	m := monitor.NewMonitor(&vanilla.Flavor{}, 30)
	callbacks := podMonitorCallbacks(m)

	// OnStartedLeading starts the monitor; a second direct StartMonitor() call must now error.
	callbacks.OnStartedLeading(context.Background())
	assert.Error(t, m.StartMonitor(), "monitor should already be started by OnStartedLeading")

	// OnStoppedLeading stops the monitor; it must not exit the process (R-02) and must leave the
	// monitor restartable.
	callbacks.OnStoppedLeading()
	assert.NoError(t, m.StartMonitor(), "monitor should be restartable after OnStoppedLeading")
	assert.NoError(t, m.StopMonitor())
}
