// Copyright 2020 Hewlett Packard Enterprise Development LP

package nodemonitor

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	monitor *NodeMonitor
)

func TestMain(m *testing.M) {
	monitor = NewNodeMonitor(nil, 60)
	code := m.Run()
	os.Exit(code)
}

func TestStartNodeMonitor(t *testing.T) {
	err := monitor.StartNodeMonitor()
	assert.Nil(t, err)
	assert.True(t, monitor.started)
	// attempt to start again and verify we error out
	err = monitor.StartNodeMonitor()
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Node monitor has already been started"))
}

func TestStopNodeMonitor(t *testing.T) {
	err := monitor.StopNodeMonitor()
	assert.Nil(t, err)
	assert.False(t, monitor.started)
	// attempt to stop again and verify we don't attempt to close channel again
	err = monitor.StopNodeMonitor()
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Node monitor has not been started"))
}
