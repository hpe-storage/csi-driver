// Copyright 2019 Hewlett Packard Enterprise Development LP
package driver

import (
	"os"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-test/pkg/sanity"

	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/concurrent"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/fake"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
)

func TestPluginSuite(t *testing.T) {
	socket := "/tmp/csi.sock"
	endpoint := "unix://" + socket
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove unix domain socket file %s, error: %s", socket, err)
	}

	log.InitLogging("csi-test.log", &log.LogParams{Level: "trace"}, false)

	// driver := realDriver(t, endpoint)
	// secretsFile := "csi-secrets.yaml"
	driver := fakeDriver(endpoint)
	secretsFile := "fake-csi-secrets.yaml"
	driver.grpc = NewNonBlockingGRPCServer()
	// start node, controller and identity servers on same endpoint for tests
	go driver.grpc.Start(driver.endpoint, driver, driver, driver)
	defer driver.Stop(true)

	stagingPath := "./csi-mnt"
	targetPath := "./csi-mnt-stage"
	os.RemoveAll(stagingPath)
	os.RemoveAll(targetPath)

	config := &sanity.Config{
		StagingPath: stagingPath,
		TargetPath:  targetPath,
		Address:     endpoint,
		SecretsFile: secretsFile,
	}

	sanity.Test(t, config)
}

// nolint: deadcode
func realDriver(t *testing.T, endpoint string) *Driver {
	driver, err := NewDriver("test-driver", "0.1", endpoint, flavor.Kubernetes, true, "", "")

	if err != nil {
		t.Fatal("Failed to initialize driver")
	}
	return driver
}

func fakeDriver(endpoint string) *Driver {
	driver := &Driver{
		name:              "fake-test-driver",
		version:           "0.1",
		endpoint:          endpoint,
		storageProviders:  make(map[string]storageprovider.StorageProvider),
		chapiDriver:       &chapi.FakeDriver{},
		flavor:            &vanilla.Flavor{},
		requestCache:      make(map[string]interface{}),
		requestCacheMutex: concurrent.NewMapMutex(),
	}

	driver.storageProviders["fake"] = fake.NewFakeStorageProvider()

	driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		//// csi.ControllerServiceCapability_RPC_LIST_VOLUMES,  // TODO: UNCOMMENT THIS ONCE LIST FEATURE IS COMPLETELY SUPPORTED
		// csi.ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		//// csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS, // TODO: UNCOMMENT THIS ONCE LIST FEATURE IS COMPLETELY SUPPORTED
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	})

	driver.AddNodeServiceCapabilities([]csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
	})

	driver.AddPluginCapabilityVolumeExpansion([]csi.PluginCapability_VolumeExpansion_Type{
		csi.PluginCapability_VolumeExpansion_ONLINE,
	})

	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
	})

	return driver
}
