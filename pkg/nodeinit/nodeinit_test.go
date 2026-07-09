// Copyright 2026 Hewlett Packard Enterprise Development LP
package nodeinit

import (
	"testing"

	"github.com/hpe-storage/common-host-libs/model"
	storage_v1 "k8s.io/api/storage/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewNodeInitContainerReturnsInstance(t *testing.T) {
	nic := NewNodeInitContainer("vanilla")
	if nic == nil {
		t.Fatalf("expected non-nil NodeInitContainer")
	}
}

func TestVolumeAttachmentSerialsForNode(t *testing.T) {
	attachments := &storage_v1.VolumeAttachmentList{
		Items: []storage_v1.VolumeAttachment{
			{
				ObjectMeta: meta_v1.ObjectMeta{Name: "va-node-1"},
				Spec: storage_v1.VolumeAttachmentSpec{
					NodeName: "node-1",
				},
				Status: storage_v1.VolumeAttachmentStatus{
					AttachmentMetadata: map[string]string{"serialNumber": "serial-1"},
				},
			},
			{
				ObjectMeta: meta_v1.ObjectMeta{Name: "va-node-2"},
				Spec: storage_v1.VolumeAttachmentSpec{
					NodeName: "node-2",
				},
				Status: storage_v1.VolumeAttachmentStatus{
					AttachmentMetadata: map[string]string{"serialNumber": "serial-2"},
				},
			},
			{
				ObjectMeta: meta_v1.ObjectMeta{Name: "va-empty-serial"},
				Spec: storage_v1.VolumeAttachmentSpec{
					NodeName: "node-1",
				},
				Status: storage_v1.VolumeAttachmentStatus{
					AttachmentMetadata: map[string]string{"serialNumber": ""},
				},
			},
		},
	}

	serials := getVolumeAttachmentSerialsForNode(attachments, "node-1")

	if len(serials) != 1 {
		t.Fatalf("expected 1 serial for node-1, got %d", len(serials))
	}
	if !doesDeviceBelongToTheNode(&model.MultipathDevice{UUID: "3serial-1"}, serials) {
		t.Fatalf("expected matching multipath UUID to belong to node")
	}
	if doesDeviceBelongToTheNode(&model.MultipathDevice{UUID: "3serial-2"}, serials) {
		t.Fatalf("expected serial from another node not to match")
	}
	if doesDeviceBelongToTheNode(&model.MultipathDevice{UUID: ""}, serials) {
		t.Fatalf("expected empty multipath UUID not to match")
	}
}
