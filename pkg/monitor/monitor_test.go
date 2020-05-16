// Copyright 2020 Hewlett Packard Enterprise Development LP

package monitor

import (
	"github.com/stretchr/testify/assert"
	"os"
	"strings"
	"testing"
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
