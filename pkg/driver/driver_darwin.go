// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

import (
	"github.com/hpe-storage/common-host-libs/chapi"
)

// returns csi driver for darwin
func getDriver(name, version, endpoint string) *Driver {
	return &Driver{
		name:             name,
		version:          version,
		endpoint:         endpoint,
		storageProviders: make(map[string]*cachedProvider),
		chapiDriver:      &chapi.MacDriver{},
	}
}
