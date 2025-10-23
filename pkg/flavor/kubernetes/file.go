// Copyright 2025 Hewlett Packard Enterprise Development LP
package kubernetes

import (
	"fmt"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/hpe-storage/common-host-libs/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	NFS              = "nfs"
	shareNfsVersion  = "shareNfsVersion"
	fileExportIPKey  = "exportIP"
	fileMountPathKey = "mountPath"
	mountOptionsKey  = "mountOptions"
)

// HandleFileNodePublish handles the NodePublishVolume request for file-based volumes in a Kubernetes environment.
// It ensures that the volume is properly mounted to the target path using NFS.
//
// Parameters:
// - req: The NodePublishVolumeRequest containing volume details, target path, and context.
//
// Steps:
// 1. Extracts the cluster IP and export path from the volume context.
// 2. Validates that the cluster IP and export path are not empty.
// 3. Constructs the NFS source and target path.
// 4. Retrieves or sets default NFS mount options.
// 5. Creates the target directory if it does not exist.
// 6. Mounts the NFS volume to the target path.
//
// Returns:
// - A NodePublishVolumeResponse on successful mount.
// - An error if any step in the process fails.

func (flavor *Flavor) HandleFileNodePublish(req *csi.NodePublishVolumeRequest, mountOptions []string) (*csi.NodePublishVolumeResponse, error) {
	log.Tracef(">>>>> HandleFileNodePublish with volume %s target path %s", req.VolumeId, req.TargetPath)
	defer log.Tracef("<<<<< HandleFileNodePublish")
	var exportIP, exportPath string
	var exportIPExists, exportPathExists bool
	exportIP, exportIPExists = req.PublishContext[fileExportIPKey]
	exportPath, exportPathExists = req.PublishContext[fileMountPathKey]
	if !exportIPExists || !exportPathExists || exportIP == "" || exportPath == "" {
		errStr := fmt.Sprintf("failed to create file provisioned volume with exportIP: %s, and mount path: %s, exportIP or mount path should not be empty ", exportIP, exportPath)
		log.Errorln(errStr)
		return nil, status.Error(codes.Internal, errStr)
	}
	source := fmt.Sprintf("%s:%s", exportIP, exportPath)
	target := req.GetTargetPath()
	mountOptions = append(mountOptions, fmt.Sprintf("addr=%s", exportIP))
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}
	if err := os.MkdirAll(target, 0750); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := flavor.chapiDriver.MountNFSVolume(source, target, mountOptions, ""); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.NodePublishVolumeResponse{}, nil
}
