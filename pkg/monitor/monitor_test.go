// Copyright 2020 Hewlett Packard Enterprise Development LP

package monitor

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/stretchr/testify/assert"
)

var (
	monitor *Monitor
)

func TestMain(m *testing.M) {
	monitor = NewMonitor(nil, 30)
	code := m.Run()
	os.Exit(code)
}

func TestStartPodMonitor(t *testing.T) {
	err := monitor.StartMonitor()
	assert.Nil(t, err)
	assert.True(t, monitor.started)
	// attempt to start again and verify we error out
	err = monitor.StartMonitor()
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Pod monitor has already been started"))
}

func TestStopPodMonitor(t *testing.T) {
	err := monitor.StopMonitor()
	assert.Nil(t, err)
	assert.False(t, monitor.started)
	// attempt to stop again and verify we don't attempt to close channel again
	err = monitor.StopMonitor()
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Pod monitor has not been started"))
}

// slowFlavor is a flavor.Flavor test double whose MonitorPod blocks until released, used to
// prove StopMonitor genuinely waits for an in-flight sweep instead of returning instantly.
type slowFlavor struct {
	flavor.Flavor
	entered chan struct{}
	release chan struct{}
}

func (f *slowFlavor) MonitorPod(ctx context.Context, podLabelkey, podLabelvalue string) error {
	close(f.entered)
	<-f.release
	return nil
}

func TestStopMonitorBlocksUntilInFlightSweepCompletes(t *testing.T) {
	sf := &slowFlavor{entered: make(chan struct{}), release: make(chan struct{})}
	m := &Monitor{flavor: sf, intervalSec: 1, podLabelKey: defaultPodMonitorLabelKey, podLabelValue: defaultPodMonitorLabelValue}
	m.stopChannel = make(chan int)
	m.done = make(chan int)
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.started = true
	assert.Nil(t, m.monitorPod())

	select {
	case <-sf.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("MonitorPod was never invoked by the monitor loop")
	}

	stopped := make(chan struct{})
	go func() {
		_ = m.StopMonitor()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("StopMonitor returned before the in-flight MonitorPod call completed")
	case <-time.After(200 * time.Millisecond):
	}

	close(sf.release)

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("StopMonitor did not return after the in-flight MonitorPod call completed")
	}
}
