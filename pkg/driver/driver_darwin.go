// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

import (
	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/storageprovider"
)

// returns csi driver for darwin
func getDriver(name, version, endpoint string) *Driver {
	return &Driver{
		name:             name,
		version:          version,
		endpoint:         endpoint,
		storageProviders: make(map[string]storageprovider.StorageProvider),
		chapiDriver:      &chapi.MacDriver{},
	}
}
