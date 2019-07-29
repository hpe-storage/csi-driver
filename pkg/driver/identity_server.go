// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/hpe-storage/common-host-libs/logger"
	"golang.org/x/net/context"
)

// GetPluginInfo ...
//
// The name says it all
func (driver *Driver) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	log.Info(">>>>> GetPluginInfo")
	defer log.Info("<<<<< GetPluginInfo")

	return &csi.GetPluginInfoResponse{
		Name:          driver.name,
		VendorVersion: driver.version,
	}, nil
}

// Probe ...
//
// A Plugin MUST implement this RPC call. The primary utility of the Probe RPC is to verify that the plugin is in a healthy and ready state.
// If an unhealthy state is reported, via a non-success response, a CO MAY take action with the intent to bring the plugin to a healthy state.
// Such actions MAY include, but SHALL NOT be limited to, the following:
// - Restarting the plugin container, or
// - Notifying the plugin supervisor.
//
// The Plugin MAY verify that it has the right configurations, devices, dependencies and drivers in order to run and return a success if the
// validation succeeds. The CO MAY invoke this RPC at any time. A CO MAY invoke this call multiple times with the understanding that a
// plugin's implementation MAY NOT be trivial and there MAY be overhead incurred by such repeated calls. The SP SHALL document guidance and known
// limitations regarding a particular Plugin's implementation of this RPC. For example, the SP MAY document the maximum frequency at which its
// Probe implementation SHOULD be called.
func (driver *Driver) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	log.Info(">>>>> Probe")
	defer log.Info("<<<<< Probe")

	return &csi.ProbeResponse{}, nil
}

// GetPluginCapabilities ...
//
// This REQUIRED RPC allows the CO to query the supported capabilities of the Plugin "as a whole": it is the grand sum of all capabilities
// of all instances of the Plugin software, as it is intended to be deployed. All instances of the same version (see vendor_version of
// GetPluginInfoResponse) of the Plugin SHALL return the same set of capabilities, regardless of both: (a) where instances are deployed on
// the cluster as well as; (b) which RPCs an instance is serving.
func (driver *Driver) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	log.Info(">>>>> GetPluginCapabilities")
	defer log.Info("<<<<< GetPluginCapabilities")

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}
