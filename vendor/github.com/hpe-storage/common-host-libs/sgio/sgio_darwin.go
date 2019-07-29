// Copyright 2019 Hewlett Packard Enterprise Development LP

package sgio

import (
	"fmt"
)

// TODO: implement the scsi ioctl for darwin
//IsGroupScopedTarget :
func IsGroupScopedTarget(device string) bool {
	return false
}

// GetDeviceSerial returns unit serial number of the device using vpd page 0x80
func GetDeviceSerial(device string) (string, error) {
	return "", nil
}

//GetNOSVersion :
func GetNOSVersion(device string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

//TestUnitReady :
func TestUnitReady(device string) error {
	return fmt.Errorf("not implemented")
}
