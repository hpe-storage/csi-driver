package chapi

import (
	"fmt"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
)

// Copyright 2019 Hewlett Packard Enterprise Development LP.

// This file is to implement NewChapiClient for windows as well as windows service control functionality

// Run windows
func Run() (err error) {
	log.Traceln("Windows chapi service is already running.")
	return nil
}

// StopChapid windows
func StopChapid() error {
	log.Traceln("No need to stop Windows chapi service.")
	return nil
}

// IsChapidRunning return true if chapid is running as part of service listening on an http port
func IsChapidRunning() bool {
	serviceName := "HPE Nimble Host Management Service"
	log.Traceln("Called IsChapidRunning to lookup service:", serviceName)

	args := []string{"-command", "Get-service", "HPE*Nimble*Host*Management*Service", "| findstr Running"}
	_, rc, err := util.ExecCommandOutput("powershell", args)
	if rc != 0 || err != nil {
		log.Tracef("Could not find running chapid service '%v' on the host. details: %v", serviceName, err.Error())
		return false
	}
	return true
}

// CheckFsCreationInProgress checks if FS creation/formatting is in progress on the device
// TODO: should be implemented for Windows for delayed create to work, so error out till then.
func CheckFsCreationInProgress(device model.Device) (inProgress bool, err error) {
	return false, fmt.Errorf("FS progress check is not implemented for Windows yet")
}
