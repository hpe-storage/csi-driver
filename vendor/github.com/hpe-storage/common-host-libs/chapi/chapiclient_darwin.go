// Copyright 2019 Hewlett Packard Enterprise Development LP

package chapi

import (
	"github.com/hpe-storage/common-host-libs/model"
	"fmt"
)

//MountFilesystem mountfilesystem
func (chapiClient *Client) MountFilesystem(volume *model.Volume, mountPoint string) error {
	return fmt.Errorf("not supported")
}