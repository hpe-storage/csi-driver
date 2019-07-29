// Copyright 2019 Hewlett Packard Enterprise Development LP

package sgio

import (
	"fmt"
)

// TODO: implement the scsi ioctl for windows
//IsGroupScopedTarget :
func IsGroupScopedTarget(device string) bool {
	return false
}

//GetNOSVersion :
func GetNOSVersion(device string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

// GetDeviceSerial returns unit serial number of the device using vpd page 0x80
func GetDeviceSerial(device string) (string, error) {
	return "", nil
}

//TestUnitReady :
func TestUnitReady(device string) error {
	return fmt.Errorf("not implemented")
}
