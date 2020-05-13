// Copyright 2020 Hewlett Packard Enterprise Development LP
package driver

import (
	"os"
	"testing"
)

func TestNodeGetIntEnv(t *testing.T) {
	driver := &Driver{
		name:    "fake-test-driver",
		version: "0.1",
	}
	os.Setenv("TEST1", "-1")
	os.Setenv("TEST2", "100")
	os.Setenv("TEST3", "one")
	os.Setenv("TEST4", "0")

	tests := []struct {
		name     string
		args     string
		expected int64
	}{
		{"Test for env1", "TEST1", -1},
		{"Test for env2", "TEST2", 100},
		{"Test for env3", "TEST3", 0},
		{"Test for env4", "TEST4", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := driver.nodeGetIntEnv(tt.args); got != tt.expected {
				t.Errorf("nodeGetIntEnv() = %v, want %v", got, tt.expected)

			}
		})
	}

}
