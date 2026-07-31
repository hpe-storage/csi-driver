// Copyright 2026 Hewlett Packard Enterprise Development LP
package driver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/fake"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
)

// lunIDProvider wraps the fake storage provider but returns a configurable
// LunID and access protocol from PublishVolume.
type lunIDProvider struct {
	*fake.StorageProvider
	lunID          int32
	accessProtocol string
	secondaryPeers []*model.SecondaryLunInfo
}

func (p *lunIDProvider) PublishVolume(id, hostUUID, accessProtocol string) (*model.PublishInfo, error) {
	proto := p.accessProtocol
	if proto == "" {
		proto = "iscsi"
	}
	info := &model.PublishInfo{
		SerialNumber: "eui.test",
	}
	info.AccessInfo.BlockDeviceAccessInfo.LunID = p.lunID
	info.AccessInfo.BlockDeviceAccessInfo.AccessProtocol = proto
	info.AccessInfo.BlockDeviceAccessInfo.TargetNames = []string{"iqn.test"}
	info.AccessInfo.BlockDeviceAccessInfo.DiscoveryIPs = []string{"10.0.0.1"}
	if p.secondaryPeers != nil {
		info.AccessInfo.BlockDeviceAccessInfo.SecondaryBackendDetails.PeerArrayDetails = p.secondaryPeers
	}
	return info, nil
}

func newDriverWithLunIDProvider(lunID int32, secondaryPeers []*model.SecondaryLunInfo) *Driver {
	return newDriverWithLunIDProviderAndProtocol(lunID, "", secondaryPeers)
}

func newDriverWithLunIDProviderAndProtocol(lunID int32, accessProtocol string, secondaryPeers []*model.SecondaryLunInfo) *Driver {
	provider := &lunIDProvider{
		StorageProvider: fake.NewFakeStorageProvider(),
		lunID:           lunID,
		accessProtocol:  accessProtocol,
		secondaryPeers:  secondaryPeers,
	}

	driver := &Driver{
		name:             "test-driver",
		version:          "0.1",
		endpoint:         "unix:///tmp/test-lunid.sock",
		storageProviders: make(map[string]storageprovider.StorageProvider),
		chapiDriver:      &chapi.FakeDriver{},
		flavor:           &vanilla.Flavor{},
	}

	credential := &storageprovider.Credentials{
		Username: "fake",
		Backend:  "fake",
	}
	cacheKey := driver.GenerateStorageProviderCacheKey(credential)
	driver.storageProviders[cacheKey] = provider

	driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
	})

	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
	})

	// Pre-create a volume so GetVolumeByID succeeds.
	provider.StorageProvider.CreateVolume("test-vol", "test volume", 1024, nil)

	return driver
}

func fakeNodeID() string {
	node := &model.Node{
		ID:   "test-node",
		UUID: "test-uuid",
		Name: "test-node",
	}
	data, _ := json.Marshal(node)
	return string(data)
}

func publishRequest(volumeID, nodeID string) *csi.ControllerPublishVolumeRequest {
	return &csi.ControllerPublishVolumeRequest{
		VolumeId: volumeID,
		NodeId:   nodeID,
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		Secrets: map[string]string{
			"backend":  "fake",
			"username": "fake",
			"password": "fake",
		},
	}
}

func TestControllerPublishVolume_NegativeLunID(t *testing.T) {
	driver := newDriverWithLunIDProvider(-1, nil)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	_, err := driver.ControllerPublishVolume(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for negative LUN ID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid LUN ID") {
		t.Errorf("error should mention invalid LUN ID, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "-1") {
		t.Errorf("error should contain the LUN ID value -1, got: %s", err.Error())
	}
}

func TestControllerPublishVolume_ZeroLunID(t *testing.T) {
	driver := newDriverWithLunIDProvider(0, nil)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	resp, err := driver.ControllerPublishVolume(context.Background(), req)
	if err != nil {
		t.Fatalf("LUN ID 0 should be valid, got error: %s", err.Error())
	}
	if resp.PublishContext["lunId"] != "0" {
		t.Errorf("expected lunId=0 in publish context, got %s", resp.PublishContext["lunId"])
	}
}

func TestControllerPublishVolume_PositiveLunID(t *testing.T) {
	driver := newDriverWithLunIDProvider(42, nil)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	resp, err := driver.ControllerPublishVolume(context.Background(), req)
	if err != nil {
		t.Fatalf("positive LUN ID should be valid, got error: %s", err.Error())
	}
	if resp.PublishContext["lunId"] != "42" {
		t.Errorf("expected lunId=42 in publish context, got %s", resp.PublishContext["lunId"])
	}
}

func TestControllerPublishVolume_NegativeSecondaryLunID(t *testing.T) {
	peers := []*model.SecondaryLunInfo{
		{
			LunID:       -1,
			TargetNames: []string{"iqn.secondary"},
		},
	}
	driver := newDriverWithLunIDProvider(5, peers)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	_, err := driver.ControllerPublishVolume(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for negative secondary LUN ID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid secondary LUN ID") {
		t.Errorf("error should mention invalid secondary LUN ID, got: %s", err.Error())
	}
}

func TestControllerPublishVolume_ValidSecondaryLunID(t *testing.T) {
	peers := []*model.SecondaryLunInfo{
		{
			LunID:       10,
			TargetNames: []string{"iqn.secondary"},
		},
	}
	driver := newDriverWithLunIDProvider(5, peers)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	resp, err := driver.ControllerPublishVolume(context.Background(), req)
	if err != nil {
		t.Fatalf("valid secondary LUN ID should succeed, got error: %s", err.Error())
	}
	if resp.PublishContext["lunId"] != "5" {
		t.Errorf("expected primary lunId=5, got %s", resp.PublishContext["lunId"])
	}
}

func TestControllerPublishVolume_NVMe_NegativeLunID(t *testing.T) {
	driver := newDriverWithLunIDProviderAndProtocol(-1, "nvmetcp", nil)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	_, err := driver.ControllerPublishVolume(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for negative LUN ID with NVMe protocol, got nil")
	}
	if !strings.Contains(err.Error(), "invalid LUN ID") {
		t.Errorf("error should mention invalid LUN ID, got: %s", err.Error())
	}
}

func TestControllerPublishVolume_NVMe_ValidLunID(t *testing.T) {
	driver := newDriverWithLunIDProviderAndProtocol(7, "nvmetcp", nil)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	resp, err := driver.ControllerPublishVolume(context.Background(), req)
	if err != nil {
		t.Fatalf("valid LUN ID with NVMe protocol should succeed, got error: %s", err.Error())
	}
	if resp.PublishContext["lunId"] != "7" {
		t.Errorf("expected lunId=7, got %s", resp.PublishContext["lunId"])
	}
}

func TestControllerPublishVolume_FC_NegativeLunID(t *testing.T) {
	driver := newDriverWithLunIDProviderAndProtocol(-1, "fc", nil)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	_, err := driver.ControllerPublishVolume(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for negative LUN ID with FC protocol, got nil")
	}
	if !strings.Contains(err.Error(), "invalid LUN ID") {
		t.Errorf("error should mention invalid LUN ID, got: %s", err.Error())
	}
}

func TestControllerPublishVolume_FC_ValidLunID(t *testing.T) {
	driver := newDriverWithLunIDProviderAndProtocol(3, "fc", nil)
	nodeID := fakeNodeID()
	req := publishRequest("test-vol", nodeID)

	resp, err := driver.ControllerPublishVolume(context.Background(), req)
	if err != nil {
		t.Fatalf("valid LUN ID with FC protocol should succeed, got error: %s", err.Error())
	}
	if resp.PublishContext["lunId"] != "3" {
		t.Errorf("expected lunId=3, got %s", resp.PublishContext["lunId"])
	}
}
