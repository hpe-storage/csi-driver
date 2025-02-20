// Copyright 2019 Hewlett Packard Enterprise Development LP
package driver

import (
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/chapi"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/fake"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
	"github.com/kubernetes-csi/csi-test/pkg/sanity"
)

const (
	testsocket string = "/tmp/csi.sock"
	testKey    string = "testKey"
	testValue  string = "testValue"
	testCount  int    = 500
)

func TestPluginSuite(t *testing.T) {
	endpoint := "unix://" + testsocket
	if err := os.Remove(testsocket); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove unix domain socket file %s, error: %s", testsocket, err)
	}

	log.InitLogging("csi-test.log", &log.LogParams{Level: "trace"}, false)

	//driver := realDriver(t, endpoint)
	//secretsFile := "csi-secrets.yaml"
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
		StagingPath:     stagingPath,
		TargetPath:      targetPath,
		Address:         endpoint,
		SecretsFile:     secretsFile,
		CreateTargetDir: createTarget,
	}

	sanity.Test(t, config)
}

func createTarget(_ string) (string, error) {
	return "./csi-mnt-stage", nil
}

//nolint:unused
func realDriver(t *testing.T, endpoint string) *Driver {
	driver, err := NewDriver("test-driver", "0.1", endpoint, flavor.Kubernetes, true, "", "", false, 0, false, 0)

	if err != nil {
		t.Fatal("Failed to initialize driver")
	}
	return driver
}

func fakeDriver(endpoint string) *Driver {
	driver := &Driver{
		name:             "fake-test-driver",
		version:          "0.1",
		endpoint:         endpoint,
		storageProviders: make(map[string]storageprovider.StorageProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
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
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
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

func TestAddRequest(t *testing.T) {
	driver := &Driver{}

	key := testKey
	value := testValue

	driver.AddRequest(key, value)

	assert.Equal(t, value, driver.GetRequest(key))
}

func TestAddRequestConcurrent(t *testing.T) {
	driver := &Driver{}

	key := testKey
	value := testValue

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			driver.AddRequest(key, value)
		}()
	}
	wg.Wait()

	assert.Equal(t, value, driver.GetRequest(key))
}

func TestClearRequest(t *testing.T) {
	driver := &Driver{}

	key := testKey
	value := testValue

	driver.AddRequest(key, value)
	driver.ClearRequest(key)

	assert.Nil(t, driver.GetRequest(key))
}

func TestAddAndClearRequestConcurrent(_ *testing.T) {
	driver := &Driver{}

	var key [testCount]string
	var value [testCount]string
	for i := 0; i < (testCount - 5); i += 5 {
		key[i] = testKey + strconv.Itoa(i)
		value[i] = testValue + strconv.Itoa(i)
		key[i+1] = testKey + strconv.Itoa(i)
		value[i+1] = testValue + strconv.Itoa(i)
		key[i+2] = testKey + strconv.Itoa(i)
		value[i+2] = testValue + strconv.Itoa(i)
		key[i+3] = testKey + strconv.Itoa(i)
		value[i+3] = testValue + strconv.Itoa(i)
		key[i+4] = testKey + strconv.Itoa(i)
		value[i+4] = testValue + strconv.Itoa(i)
	}

	var wg sync.WaitGroup
	for i := 0; i < testCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			driver.AddRequest(key[i], value[i])
			driver.HandleDuplicateRequest(key[i])
			rand.Seed(time.Now().UnixNano())
			n := rand.Intn(1000) // n will be between 0 and 10
			time.Sleep(time.Duration(n) * time.Microsecond)
			driver.ClearRequest(key[i])
		}(i)
	}
	wg.Wait()

}

func TestClearRequestConcurrent(_ *testing.T) {
	driver := &Driver{}

	var key [testCount]string
	var value [testCount]string
	for i := 0; i < (testCount - 5); i += 5 {
		key[i] = testKey + strconv.Itoa(i)
		value[i] = testValue + strconv.Itoa(i)
		key[i+1] = testKey + strconv.Itoa(i)
		value[i+1] = testValue + strconv.Itoa(i)
		key[i+2] = testKey + strconv.Itoa(i)
		value[i+2] = testValue + strconv.Itoa(i)
		key[i+3] = testKey + strconv.Itoa(i)
		value[i+3] = testValue + strconv.Itoa(i)
		key[i+4] = testKey + strconv.Itoa(i)
		value[i+4] = testValue + strconv.Itoa(i)
	}

	var wg sync.WaitGroup
	for i := 0; i < testCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			driver.HandleDuplicateRequest(key[i])
			rand.Seed(time.Now().UnixNano())
			n := rand.Intn(1000) // n will be between 0 and 10
			time.Sleep(time.Duration(n) * time.Microsecond)
			driver.ClearRequest(key[i])
		}(i)
	}
	wg.Wait()

}
