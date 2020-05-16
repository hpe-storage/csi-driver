// Copyright 2019 Hewlett Packard Enterprise Development LP
package driver

import (
	"os"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/concurrent"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/fake"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
	"github.com/kubernetes-csi/csi-test/pkg/sanity"
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
	driver, err := NewDriver("test-driver", "0.1", endpoint, flavor.Kubernetes, true, "", "", false, 0)

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

	credential := &storageprovider.Credentials{
		Username: "fake",
		Backend:  "fake",
	}
	cacheKey := driver.GenerateStorageProviderCacheKey(credential)
	driver.storageProviders[cacheKey] = fake.NewFakeStorageProvider()

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

func TestGenerateStorageProviderCacheKey(t *testing.T) {
	driver := &Driver{
		name:    "fake-test-driver",
		version: "0.1",
	}

	cred1 := &storageprovider.Credentials{
		Username: "admin",
		Password: "password",
		Backend:  "1.1.1.1",
	}
	cred2 := &storageprovider.Credentials{
		Username: "user",
		Password: "password",
		Backend:  "1.1.1.1",
	}
	cred3 := &storageprovider.Credentials{
		Username: "test",
		Password: "password",
		Backend:  "1.1.1.1",
	}
	cred4 := &storageprovider.Credentials{
		Username: "test",
		Password: "password",
		Backend:  "2.2.2.2",
	}
	cred5 := &storageprovider.Credentials{
		Username: "user",
		Password: "password",
		Backend:  "2.2.2.2",
	}

	type args struct {
		credential *storageprovider.Credentials
	}

	tests := []struct {
		name     string
		args     args
		expected string
		fails    bool
	}{
		{"Test for user1", args{cred1}, "6903fab9e2797c294e1c4a0b87a5f296acb887f946d471ee90d0f6caeecd42ea", false},
		{"Test for user1", args{cred1}, "6903fab9e2797c294e1c4a0b87a5f296acb887f946d471ee90d0f6caeecd42", true},
		{"Test for user2", args{cred2}, "7768c1adea41b7cacff35a4648d98a47372c1b14c7843712b5414092290dd26b", false},
		{"Test for user3", args{cred3}, "eebff4d52bb48b871bf5b63b15d3deee6efb9997a067a4aac39ff0601cb41c39", false},
		{"Test for user4", args{cred4}, "cc3302d1062f0561eddba8f91606dab0688ac676ffc8fefcc0c827b206803b81", false},
		{"Test for user5", args{cred5}, "03fa2eb6d209f927fa680ec64275a8242c74a82d3d44aea7610fc5358732b6b5", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := driver.GenerateStorageProviderCacheKey(tt.args.credential); got != tt.expected {
				if tt.fails == false {
					t.Errorf("GenerateStorageProviderCacheKey() = %v, want %v", got, tt.expected)
				}
			}
		})
	}
}
