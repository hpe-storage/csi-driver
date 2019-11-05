// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"

	csi "github.com/container-storage-interface/spec/lib/go/csi/v0"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/stringformat"
	"github.com/hpe-storage/common-host-libs/util"
)

// Helper utility to construct default mountpoint path
func getDefaultMountPoint(id string) string {
	return fmt.Sprintf("%s/%s", defaultMountDir, id)
}

func getMountInfo(volumeID string, volCap *csi.VolumeCapability, publishContext map[string]string) *Mount {
	log.Tracef(">>>>> getMountInfo, volumeID: %s", volumeID)
	defer log.Trace("<<<<< getMountInfo")

	// Get Mount options from the requested volume capability and read-only flag
	mountOptions := getMountOptionsFromVolCap(volCap)
	// Check if 'Read-Only' is set in the Publish context (By ControllerPublish)
	if publishContext[readOnly] == "true" {
		log.Trace("Adding'read-only' mount option from the Publish context")
		mountOptions = append(mountOptions, "ro")
	}

	// Read filesystem info from the publish context
	fsOpts := &model.FilesystemOpts{
		Type:  publishContext[filesystemType],
		Mode:  publishContext[filesystemMode],
		Owner: publishContext[filesystemOwner],
	}

	return &Mount{
		MountPoint:        getDefaultMountPoint(volumeID), // Default staging mountpoint
		MountOptions:      mountOptions,
		FilesystemOptions: fsOpts,
	}
}

// getMountOptionsFromVolCap returns the mount options from the VolumeCapability if any
func getMountOptionsFromVolCap(volCap *csi.VolumeCapability) (mountOptions []string) {
	if volCap.GetMount() != nil && len(volCap.GetMount().MountFlags) != 0 {
		mountOptions = volCap.GetMount().MountFlags
	}
	return mountOptions
}

// isMounted returns true if its mounted else false
func (driver *Driver) isMounted(device *model.Device, mountPoint string) (bool, error) {
	log.Tracef(">>>>> isMounted, device: %+v, mountPoint: %s", device, mountPoint)
	defer log.Trace("<<<<< isMounted")

	// Get all mounts for device
	mounts, err := driver.chapiDriver.GetMountsForDevice(device)
	if err != nil {
		return false, status.Error(codes.Internal, fmt.Sprintf("Error retrieving mounts for the device %s", device.AltFullPathName))
	}
	for _, mount := range mounts {
		if mount.Mountpoint == mountPoint {
			return true, nil
		}
	}
	return false, nil
}

// NodeStageVolume ...
//
// A Node Plugin MUST implement this RPC call if it has STAGE_UNSTAGE_VOLUME node capability.
//
// This RPC is called by the CO prior to the volume being consumed by any workloads on the node by NodePublishVolume. The Plugin SHALL assume
// that this RPC will be executed on the node where the volume will be used. This RPC SHOULD be called by the CO when a workload that wants to
// use the specified volume is placed (scheduled) on the specified node for the first time or for the first time since a NodeUnstageVolume call
// for the specified volume was called and returned success on that node.
//
// If the corresponding Controller Plugin has PUBLISH_UNPUBLISH_VOLUME controller capability and the Node Plugin has STAGE_UNSTAGE_VOLUME
// capability, then the CO MUST guarantee that this RPC is called after ControllerPublishVolume is called for the given volume on the given node
// and returns a success. The CO MUST guarantee that this RPC is called and returns a success before any NodePublishVolume is called for the given
// volume on the given node.
//
// This operation MUST be idempotent. If the volume corresponding to the volume_id is already staged to the staging_target_path, and is identical
// to the specified volume_capability the Plugin MUST reply 0 OK.
//
// If this RPC failed, or the CO does not know if it failed or not, it MAY choose to call NodeStageVolume again, or choose to call
// NodeUnstageVolume.
// nolint: gocyclo
func (driver *Driver) NodeStageVolume(ctx context.Context, request *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	log.Trace(">>>>> NodeStageVolume")
	defer log.Trace("<<<<< NodeStageVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume ID specified for NodeStageVolume")
	}

	if request.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid staging target path specified for NodeStageVolume")
	}

	if request.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume capability specified for NodeStageVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s", "NodeStageVolume", request.VolumeId, request.StagingTargetPath)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Validate Volume Capability
	log.Tracef("Validating volume capability: %+v", request.VolumeCapability)
	_, err := driver.IsValidVolumeCapability(request.VolumeCapability)
	if err != nil {
		log.Errorf("Found unsupported volume capability %+v", request.VolumeCapability)
		return nil, err
	}

	// Get volume access type (Block or Mount)
	volAccessType, err := driver.getVolumeAccessType(request.VolumeCapability)
	if err != nil {
		log.Errorf("Failed to retrieve volume access type, err: %v", err.Error())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to retrieve volume access type, %v", err.Error()))
	}

	// Controller published volume access type must match with the requested volcap
	if volAccessType.String() != request.PublishInfo[volumeAccessMode] {
		log.Errorf("Controller published volume access type %v mismatched with the requested access type %v",
			request.PublishInfo[volumeAccessMode], volAccessType.String())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Controller already published the volume with access type %v, but node staging requested with access type %v",
				request.PublishInfo[volumeAccessMode], volAccessType.String()))
	}

	log.Infof("NodeStageVolume requested volume %s with access type %s, targetPath %s, capability %v, publishContext %v and volumeContext %v",
		request.VolumeId, volAccessType.String(), request.StagingTargetPath, request.VolumeCapability, request.PublishInfo, request.VolumeAttributes)

	// Get Volume
	if _, err := driver.GetVolumeByID(request.VolumeId, request.NodeStageSecrets); err != nil {
		log.Error("Failed to get volume ", request.VolumeId)
		return nil, err // NOT_FOUND
	}

	// Stage the volume on the node by creating a new device with block or mount access.
	// If already staged, then validate it and return appropriate response.

	// Check if the volume has already been staged. If yes, then return here with success
	staged, err := driver.isVolumeStaged(request, volAccessType)
	if err != nil {
		log.Errorf("Error validating the staged info for volume %s, err: %v", request.VolumeId, err.Error())
		return nil, err
	}
	if staged {
		log.Infof("Volume %s has already been staged. Returning here", request.VolumeId)
		return &csi.NodeStageVolumeResponse{}, nil // volume already staged, do nothing and return here
	}

	// Stage volume - Create device and expose volume as raw block or mounted directory (filesystem)
	log.Tracef("NodeStageVolume staging volume %s to the staging path %s",
		request.VolumeId, request.StagingTargetPath)
	stagingDev, err := driver.stageVolume(request, volAccessType)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to stage volume %s, Err: %s",
			request.VolumeId, err.Error()))
	}
	log.Tracef("Staged volume %s successfully, StagingDev: %#v", request.VolumeId, stagingDev)

	// Save staged device info in the staging area
	log.Tracef("NodeStageVolume writing device info %+v to staging target path %s",
		stagingDev.Device, request.StagingTargetPath)
	err = writeStagedDeviceInfo(request.StagingTargetPath, stagingDev)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to stage volume %s, Err: %s",
			request.VolumeId, err.Error()))
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

// isVolumeStaged checks if the volume is already been staged on the node.
// If already staged, then returns true else false
func (driver *Driver) isVolumeStaged(request *csi.NodeStageVolumeRequest, volAccessType model.VolumeAccessType) (bool, error) {
	log.Tracef(">>>>> isVolumeStaged, volumeID: %s, stagingPath: %s, volAccessType: %s",
		request.VolumeId, request.StagingTargetPath, volAccessType.String())
	defer log.Trace("<<<<< isVolumeStaged")

	// Check if the staged device file exists
	filePath := path.Join(request.StagingTargetPath, deviceInfoFileName)
	exists, _, _ := util.FileExists(filePath)
	if !exists {
		return false, nil // Not staged as file doesn't exist
	}

	// Read the device info from the staging path
	stagingDev, _ := readStagedDeviceInfo(request.StagingTargetPath)
	if stagingDev == nil {
		return false, nil // Not staged as device info not found
	}
	log.Tracef("Found staged device: %+v", stagingDev)

	// Check if the volume ID matches with the device info (already staged)
	if request.VolumeId != stagingDev.VolumeID {
		log.Errorf("Volume %s is not matching with the staged device's volume ID %s",
			request.VolumeId, stagingDev.VolumeID)
		return false, nil // Not staged as volume id mismatch
	}

	if volAccessType == model.MountType {
		// Check if the staged device exists. If error (i.e, device is missing), then return as 'Not staged'
		mounts, err := driver.chapiDriver.GetMountsForDevice(stagingDev.Device)
		if err != nil || len(mounts) == 0 {
			log.Infof("Device %+v not present on the host", stagingDev.Device)
			return false, nil // Not staged as device doesn't exist on the host
		}
		log.Tracef("Found %v mounts for device with serial number %v", len(mounts), stagingDev.Device.SerialNumber)

		foundMount := false
		for _, mount := range mounts {
			if stagingDev.MountInfo != nil && mount.Mountpoint == stagingDev.MountInfo.MountPoint {
				foundMount = true
				break
			}
		}
		if !foundMount {
			log.Infof("Device %+v not mounted on the host", stagingDev.Device)
			return false, nil
		}
	}

	if volAccessType == model.BlockType {
		// Check if path exists
		exists, _, _ := util.FileExists(stagingDev.Device.AltFullPathName)
		if !exists {
			log.Infof("Device path %s does not exist on the node", stagingDev.Device.AltFullPathName)
			return false, nil // Not staged yet as device path does not exists
		}
	}

	// Validate the requested mount details with the staged device details
	if volAccessType == model.MountType && stagingDev.MountInfo != nil {
		mountInfo := getMountInfo(request.VolumeId, request.VolumeCapability, request.PublishInfo)

		log.Tracef("Checking for mount options compatibility: staged options: %v, reqMountOptions: %v",
			stagingDev.MountInfo.MountOptions, mountInfo.MountOptions)
		// Check if reqMountOptions are compatible with stagingDev mount options
		if !stringformat.StringsLookup(stagingDev.MountInfo.MountOptions, mountInfo.MountOptions) {
			// This means staging has not mounted the device with appropriate mount options for the PVC
			log.Errorf("Mount flags %v are not compatible with the staged mount options %v",
				mountInfo.MountOptions, stagingDev.MountInfo.MountOptions)
			return false, status.Error(
				codes.AlreadyExists,
				fmt.Sprintf("Volume %s has already been staged at the specified staging target_path %s but is incompatible with the specified volume_capability",
					request.VolumeId, request.StagingTargetPath))
		}
		// TODO: Check for fsOpts compatibility
	}

	return true, nil
}

// stageVolumeOnNode performs all the necessary tasks to stage the volume on the node to the staging path
func (driver *Driver) stageVolume(request *csi.NodeStageVolumeRequest, volAccessType model.VolumeAccessType) (*StagingDevice, error) {
	log.Tracef(">>>>> stageVolume, volumeID: %s, volAccessType: %v", request.VolumeId, volAccessType.String())
	defer log.Trace("<<<<< stageVolume")

	// Create device for volume on the node
	device, err := driver.setupDevice(request.PublishInfo)
	if err != nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("Error creating device for volume %s, err: %v", request.VolumeId, err.Error()))
	}
	log.Infof("Device setup successful, Device: %+v", device)

	// Construct staging device to be stored in the staging path on the node
	stagingDevice := &StagingDevice{
		VolumeID:         request.VolumeId,
		VolumeAccessMode: volAccessType,
		Device:           device,
	}

	// If Block, then stage the volume for raw block device access
	if volAccessType == model.BlockType {
		// Do nothing
		return stagingDevice, nil
	}

	// If Mount, then stage the volume for filesystem access

	// Get mount info from the request
	mountInfo := getMountInfo(request.VolumeId, request.VolumeCapability, request.PublishInfo)

	// Create Filesystem, Mount Device, Apply FS options and Apply Mount options
	mount, err := driver.chapiDriver.MountDevice(device, mountInfo.MountPoint,
		mountInfo.MountOptions, mountInfo.FilesystemOptions)
	if err != nil {
		return nil, fmt.Errorf("Failed to mount device %s, %v", device.AltFullPathName, err.Error())
	}
	log.Tracef("Device %s mounted successfully, Mount: %+v", device.AltFullPathName, mount)

	// Store mount info in the staging device
	stagingDevice.MountInfo = mountInfo

	return stagingDevice, nil
}

func (driver *Driver) setupDevice(publishContext map[string]string) (*model.Device, error) {
	log.Tracef(">>>>> setupDevice, publishContext: %v", publishContext)
	defer log.Trace("<<<<< setupDevice")

	// TODO: Enhance CHAPI to work with a PublishInfo object rather than a volume

	discoveryIps := strings.Split(publishContext[discoveryIPs], ",")
	log.Tracef("Selecting the first discovery IP of %s from all discovery IPs %s", discoveryIps[0], discoveryIps)

	volume := &model.Volume{
		SerialNumber:   publishContext[serialNumber],
		AccessProtocol: publishContext[accessProtocol],
		Iqn:            publishContext[targetName],
		TargetScope:    publishContext[targetScope],
		LunID:          publishContext[lunID],
		DiscoveryIP:    discoveryIps[0],
	}
	if publishContext[accessProtocol] == iscsi {
		chapInfo := &model.ChapInfo{
			Name:     publishContext[chapUsername],
			Password: publishContext[chapPassword],
		}
		volume.Chap = chapInfo
	}

	// Create Device
	devices, err := driver.chapiDriver.CreateDevices([]*model.Volume{volume})
	if err != nil {
		log.Errorf("Failed to create device from publish info. Error: %s", err.Error())
		return nil, err
	}
	if len(devices) == 0 {
		log.Errorf("Failed to get the device just created using the volume %+v", volume)
		return nil, fmt.Errorf("Unable to find the device for volume %+v", volume)
	}

	// Update targetScope in stagingDevice from publishContext.
	// This is useful during unstage to let CHAPI know to disconnect target or not(GST).
	// TODO: let chapi populate targetScope on attached devices.
	if scope, ok := publishContext[targetScope]; ok {
		devices[0].TargetScope = scope
	}

	return devices[0], nil
}

// NodeUnstageVolume ...
//
// A Node Plugin MUST implement this RPC call if it has STAGE_UNSTAGE_VOLUME node capability.
//
// This RPC is a reverse operation of NodeStageVolume. This RPC MUST undo the work by the corresponding NodeStageVolume. This RPC SHALL be
// called by the CO once for each staging_target_path that was successfully setup via NodeStageVolume.
//
// If the corresponding Controller Plugin has PUBLISH_UNPUBLISH_VOLUME controller capability and the Node Plugin has STAGE_UNSTAGE_VOLUME
// capability, the CO MUST guarantee that this RPC is called and returns success before calling ControllerUnpublishVolume for the given node
// and the given volume. The CO MUST guarantee that this RPC is called after all NodeUnpublishVolume have been called and returned success for
// the given volume on the given node.
//
// The Plugin SHALL assume that this RPC will be executed on the node where the volume is being used.
//
// This RPC MAY be called by the CO when the workload using the volume is being moved to a different node, or all the workloads using the volume
// on a node have finished.
//
// This operation MUST be idempotent. If the volume corresponding to the volume_id is not staged to the staging_target_path, the Plugin MUST
// reply 0 OK.
//
// If this RPC failed, or the CO does not know if it failed or not, it MAY choose to call NodeUnstageVolume again.
// nolint: gocyclo
func (driver *Driver) NodeUnstageVolume(ctx context.Context, request *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	log.Trace(">>>>> NodeUnstageVolume")
	defer log.Trace("<<<<< NodeUnstageVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume ID specified for NodeUnstageVolume")
	}

	if request.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid staging target path specified for NodeUnstageVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s", "NodeUnstageVolume", request.VolumeId, request.StagingTargetPath)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	log.Infof("NodeUnstageVolume requested volume %s with targetPath %s", request.VolumeId, request.StagingTargetPath)

	// Check if the staged device file exists
	deviceFilePath := path.Join(request.StagingTargetPath, deviceInfoFileName)
	exists, _, _ := util.FileExists(deviceFilePath)
	if !exists {
		log.Infof("Volume %s not in staged state as the device info file %s does not exist. Returning here",
			request.VolumeId, deviceFilePath)
		return &csi.NodeUnstageVolumeResponse{}, nil // Already unstaged as device file doesn't exist
	}

	// Read the device info from the staging path if exists
	stagingDev, _ := readStagedDeviceInfo(request.StagingTargetPath)
	if stagingDev == nil {
		log.Infof("Volume %s not in staged state as the staging device info does not exist. Returning here",
			request.VolumeId)
		return &csi.NodeUnstageVolumeResponse{}, nil // Already unstaged as device info doesn't exist
	}
	log.Tracef("Found staged device info: %+v", stagingDev)

	device := stagingDev.Device
	if device == nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("Missing device info in the staging device %v", stagingDev))
	}

	// If mounted, then unmount the filesystem
	if stagingDev.VolumeAccessMode == model.MountType && stagingDev.MountInfo != nil {
		// Unmount the device from the mountpoint
		_, err := driver.chapiDriver.UnmountDevice(stagingDev.Device, stagingDev.MountInfo.MountPoint)
		if err != nil {
			log.Errorf("Failed to unmount device %s from mountpoint %s, err: %s",
				device.AltFullPathName, stagingDev.MountInfo.MountPoint, err.Error())
			return nil, status.Error(codes.Internal,
				fmt.Sprintf("Error unmounting device %s from mountpoint %s, err: %s",
					device.AltFullPathName, stagingDev.MountInfo.MountPoint, err.Error()))
		}
	}

	// Delete device
	log.Tracef("NodeUnstageVolume deleting device %+v", device)
	if err := driver.chapiDriver.DeleteDevice(device); err != nil {
		log.Errorf("Failed to delete device with path name %s.  Error: %s", device.Pathname, err.Error())
		return nil, status.Error(codes.Internal, "Error deleting device "+device.Pathname)
	}
	// Remove the device file (optional)
	removeStagedDeviceInfo(request.StagingTargetPath)

	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume ...
//
// This RPC is called by the CO when a workload that wants to use the specified volume is placed (scheduled) on a node. The Plugin SHALL assume
// that this RPC will be executed on the node where the volume will be used.
//
// If the corresponding Controller Plugin has PUBLISH_UNPUBLISH_VOLUME controller capability, the CO MUST guarantee that this RPC is called after
// ControllerPublishVolume is called for the given volume on the given node and returns a success.
//
// This operation MUST be idempotent. If the volume corresponding to the volume_id has already been published at the specified target_path, and is
// compatible with the specified volume_capability and readonly flag, the Plugin MUST reply 0 OK.
//
// If this RPC failed, or the CO does not know if it failed or not, it MAY choose to call NodePublishVolume again, or choose to call
// NodeUnpublishVolume.
//
// This RPC MAY be called by the CO multiple times on the same node for the same volume with possibly different target_path and/or other arguments
// if the volume has MULTI_NODE capability (i.e., access_mode is either MULTI_NODE_READER_ONLY, MULTI_NODE_SINGLE_WRITER or MULTI_NODE_MULTI_WRITER).
// The following table shows what the Plugin SHOULD return when receiving a second NodePublishVolume on the same volume on the same node:
//
// 					T1=T2, P1=P2		T1=T2, P1!=P2		T1!=T2, P1=P2			T1!=T2, P1!=P2
// MULTI_NODE		OK (idempotent)		ALREADY_EXISTS		OK						OK
// Non MULTI_NODE	OK (idempotent)		ALREADY_EXISTS		FAILED_PRECONDITION		FAILED_PRECONDITION
func (driver *Driver) NodePublishVolume(ctx context.Context, request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Trace(">>>>> NodePublishVolume")
	defer log.Trace("<<<<< NodePublishVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume ID specified for NodePublishVolume")
	}

	if request.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid target path specified for NodePublishVolume")
	}

	if request.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid staging target path specified for NodePublishVolume")
	}

	if request.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume capability specified for NodePublishVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s:%s", "NodePublishVolume", request.VolumeId, request.TargetPath, request.StagingTargetPath)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Validate Capability
	log.Tracef("Validating volume capability: %+v", request.VolumeCapability)
	_, err := driver.IsValidVolumeCapability(request.VolumeCapability)
	if err != nil {
		log.Errorf("Found unsupported volume capability %+v", request.VolumeCapability)
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Unsupported volume capability %+v specified for NodePublishVolume", request.VolumeCapability))
	}
	// Get volume access type
	volAccessType, err := driver.getVolumeAccessType(request.VolumeCapability)
	if err != nil {
		log.Errorf("Failed to retrieve volume access type, err: %v", err.Error())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to retrieve volume access type, %v", err.Error()))
	}

	// Controller published volume access type must match with the requested volcap
	if volAccessType.String() != request.PublishInfo[volumeAccessMode] {
		log.Errorf("Controller published volume access type '%v' mismatched with the requested access type '%v'",
			request.PublishInfo[volumeAccessMode], volAccessType.String())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Controller already published the volume with access type %v, but node publish requested with access type %v",
				request.PublishInfo[volumeAccessMode], volAccessType.String()))
	}

	log.Infof("NodePublishVolume requested volume %s with access type %s, targetPath %s, capability %v, publishContext %v and volumeContext %v",
		request.VolumeId, volAccessType, request.StagingTargetPath, request.VolumeCapability, request.PublishInfo, request.VolumeAttributes)

	// Get Volume
	if _, err = driver.GetVolumeByID(request.VolumeId, request.NodePublishSecrets); err != nil {
		log.Error("Failed to get volume ", request.VolumeId)
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Node publish the volume to the target path with block or mount access.
	// If already published, then validate it and return appropriate response.

	// Read device info from the staging area
	stagingDev, err := readStagedDeviceInfo(request.StagingTargetPath)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition,
			fmt.Sprintf("Staging target path %s not set, Err: %s", request.TargetPath, err.Error()))
	}

	// Check if the volume has already published on the targetPath
	published, err := driver.isVolumePublished(request, stagingDev)
	if err != nil {
		log.Errorf("Error while validating the published info for volume %s, err: %v",
			request.VolumeId, err.Error())
		return nil, err
	}
	if published {
		log.Infof("The target path %s has already been published for volume %s. Returning here",
			request.TargetPath, request.VolumeId)
		return &csi.NodePublishVolumeResponse{}, nil // VOLUME TARGET ALREADY PUBLISHED
	}

	// If Block, then stage the volume for raw block device access
	if volAccessType == model.BlockType {
		log.Tracef("Publishing the volume for raw block access (create symlink), devicePath: %s, targetPath: %v",
			stagingDev.Device.AltFullPathName, request.TargetPath)
		// Note: Bind-mount is not allowed for raw block device as there is no filesystem on it.
		//       So, we create softlink to the device file. TODO: mknode() instead ???
		//       Ex: ln -s /dev/mpathbm <targetPath>
		if err := os.Symlink(stagingDev.Device.AltFullPathName, request.TargetPath); err != nil {
			log.Errorf("Failed to create symlink %s to the device path %s, err: %v",
				request.TargetPath, stagingDev.Device.AltFullPathName, err.Error())
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		log.Tracef("Publishing the volume for filesystem access, stagedPath: %s, targetPath: %v",
			stagingDev.MountInfo.MountPoint, request.TargetPath)
		// Publish: Bind mount the staged mountpoint on the target path
		if err = driver.chapiDriver.BindMount(stagingDev.MountInfo.MountPoint, request.TargetPath, false); err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to publish volume %s to the target path %s, err: %v", request.VolumeId, request.TargetPath, err.Error()))
		}
	}
	log.Tracef("Published the volume %s to the target path %s successfully", request.VolumeId, request.TargetPath)

	return &csi.NodePublishVolumeResponse{}, nil
}

// isVolumePublished returns true if the volume is already published, else returns false
func (driver *Driver) isVolumePublished(request *csi.NodePublishVolumeRequest, stagingDev *StagingDevice) (bool, error) {
	log.Tracef(">>>>> isVolumePublished, volumeID: %s, stagingDev: %+v, targetPath: %s",
		request.VolumeId, stagingDev, request.TargetPath)
	defer log.Trace("<<<<< isVolumePublished")

	// Check for volume ID match with the staged device info
	if stagingDev.VolumeID != request.VolumeId {
		return false, nil // Not published yet as volume id mismatch
	}
	if stagingDev.Device == nil {
		return false, nil // Not published yet as device info not found
	}

	// If Block, then check if the target path exists on the node
	if stagingDev.VolumeAccessMode == model.BlockType {
		// Check if the device already published (softlink) to the target path
		log.Tracef("Checking if the device %s already published to the target path %s",
			stagingDev.Device.AltFullPathName, request.TargetPath)
		// Check if path exists
		exists, _, _ := util.FileExists(request.TargetPath)
		if !exists {
			log.Tracef("Target path %s does not exist on the node", request.TargetPath)
			return false, nil // Not published yet as device info not found
		}
		// TODO: Check if it is mapped to the device file
		log.Tracef("Target path %s exists on the node", request.TargetPath)
		return true, nil // Published
	}

	// Else Mount:
	// Check if the volume already published (bind-mounted) to the target path
	log.Tracef("Checking if the volume %s already bind-mounted to the target path %s",
		request.VolumeId, request.TargetPath)
	mounted, err := driver.isMounted(stagingDev.Device, request.TargetPath)
	if err != nil {
		log.Errorf("Error while checking if device %s is bind-mounted to target path %s, Err: %s",
			stagingDev.Device.AltFullPathName, request.TargetPath, err.Error())
		return false, err
	}
	if !mounted {
		log.Tracef("Mountpoint %s is not bind-mounted yet", request.TargetPath)
		return false, nil // Not published yet as bind-mount path does not exist
	}

	// If mount access type, then validate the requested mount details with the staged device details
	if stagingDev.MountInfo != nil {
		// Get mount info from the request
		mountInfo := getMountInfo(request.VolumeId, request.VolumeCapability, request.PublishInfo)

		// Check if reqMountOptions are compatible with staged device's mount options
		log.Tracef("Checking for mount options compatibility: staged options: %v, reqMountOptions: %v",
			stagingDev.MountInfo.MountOptions, mountInfo.MountOptions)
		if !stringformat.StringsLookup(stagingDev.MountInfo.MountOptions, mountInfo.MountOptions) {
			// This means staging has not mounted the device with appropriate mount options for the PVC
			log.Errorf("Mount flags %v are not compatible with the staged mount options %v", mountInfo.MountOptions, stagingDev.MountInfo.MountOptions)
			return false, status.Error(codes.AlreadyExists,
				fmt.Sprintf("Volume %s has already been published at the specified staging target_path %s but is incompatible with the specified volume_capability and readonly flag",
					request.VolumeId, request.TargetPath))
		}
		// TODO: Check for fsOpts compatibility
	}
	return true, nil // Published
}

// NodeUnpublishVolume ...
//
// A Node Plugin MUST implement this RPC call. This RPC is a reverse operation of NodePublishVolume. This RPC MUST undo the work by the
// corresponding NodePublishVolume. This RPC SHALL be called by the CO at least once for each target_path that was successfully setup via
// NodePublishVolume. If the corresponding Controller Plugin has PUBLISH_UNPUBLISH_VOLUME controller capability, the CO SHOULD issue all
// NodeUnpublishVolume (as specified above) before calling ControllerUnpublishVolume for the given node and the given volume. The Plugin
// SHALL assume that this RPC will be executed on the node where the volume is being used.
//
// This RPC is typically called by the CO when the workload using the volume is being moved to a different node, or all the workload using
// the volume on a node has finished.
//
// This operation MUST be idempotent. If this RPC failed, or the CO does not know if it failed or not, it can choose to call NodeUnpublishVolume
// again.
func (driver *Driver) NodeUnpublishVolume(ctx context.Context, request *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Trace(">>>>> NodeUnpublishVolume")
	defer log.Trace("<<<<< NodeUnpublishVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume ID specified for NodeUnpublishVolume")
	}

	if request.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid target path specified for NodeUnpublishVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s", "NodeUnpublishVolume", request.VolumeId, request.TargetPath)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Check if path exists
	exists, _, _ := util.FileExists(request.TargetPath)
	if exists {
		// Block volume: Check for symlink and remove it
		_, symlink, _ := util.IsFileSymlink(request.TargetPath)
		if symlink {
			// Remove the symlink
			log.Tracef("Removing the symlink from target path %s", request.TargetPath)
			if err := util.FileDelete(request.TargetPath); err != nil {
				return nil, status.Error(codes.Internal, "Error removing the symlink target path "+request.TargetPath)
			}
			return &csi.NodeUnpublishVolumeResponse{}, nil
		}

		// Else Mount volume: Unmount the filesystem
		log.Trace("Unmounting filesystem from target path " + request.TargetPath)
		_, err := driver.chapiDriver.UnmountFileSystem(request.TargetPath)
		if err != nil {
			return nil, status.Error(codes.Internal, "Error unmounting target path "+request.TargetPath)
		}
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetId ...
// deprecated
// nolint: golint
func (driver *Driver) NodeGetId(ctx context.Context, req *csi.NodeGetIdRequest) (*csi.NodeGetIdResponse, error) {
	log.Info(">>>>> NodeGetId")
	defer log.Info("<<<<< NodeGetId")

	nodeID, err := driver.nodeGetInfo()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeGetIdResponse{
		NodeId: nodeID,
	}, nil
}

// NodeGetInfo ...
//
// A Node Plugin MUST implement this RPC call if the plugin has PUBLISH_UNPUBLISH_VOLUME controller capability. The Plugin SHALL assume that
// this RPC will be executed on the node where the volume will be used. The CO SHOULD call this RPC for the node at which it wants to place
// the workload. The CO MAY call this RPC more than once for a given node. The SP SHALL NOT expect the CO to call this RPC more than once. The
// result of this call will be used by CO in ControllerPublishVolume.
func (driver *Driver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	log.Trace(">>>>> NodeGetInfo")
	defer log.Trace("<<<<< NodeGetInfo")

	nodeID, err := driver.nodeGetInfo()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeGetInfoResponse{
		NodeId: nodeID,
	}, nil
}

// nolint: gocyclo
func (driver *Driver) nodeGetInfo() (string, error) {
	hosts, err := driver.chapiDriver.GetHosts()
	if err != nil {
		return "", errors.New("Failed to get host from chapi driver")
	}
	host := (*hosts)[0]

	hostNameAndDomain, err := driver.chapiDriver.GetHostNameAndDomain()
	if err != nil {
		log.Error("Failed to get host name and domain")
		return "", errors.New("Failed to get host name and domain host")
	}
	log.Infof("Host name reported as %s", hostNameAndDomain[0])

	initiators, err := driver.chapiDriver.GetHostInitiators()
	if err != nil {
		log.Errorf("Failed to get initiators for host %s.  Error: %s", hostNameAndDomain[0], err.Error())
		return "", errors.New("Failed to get initiators for host")
	}

	networks, err := driver.chapiDriver.GetHostNetworks()
	if err != nil {
		log.Errorf("Failed to get networks for host %s.  Error: %s", hostNameAndDomain[0], err.Error())
		return "", errors.New("Failed to get networks for host")
	}

	var iqns []*string
	var wwpns []*string
	for _, initiator := range initiators {
		if initiator.Type == iscsi {
			for i := 0; i < len(initiator.Init); i++ {
				iqns = append(iqns, &initiator.Init[i])
			}
		} else {
			for i := 0; i < len(initiator.Init); i++ {
				wwpns = append(wwpns, &initiator.Init[i])
			}
		}
	}

	var computedNetworks []*string
	for _, network := range networks {
		log.Infof("Processing network named %s with IP %s and mask %s", network.Name, network.AddressV4, network.MaskV4)
		if network.AddressV4 != "" {
			ip := net.ParseIP(network.AddressV4)
			maskIP := net.ParseIP(network.MaskV4)
			mask := net.IPv4Mask(maskIP[12], maskIP[13], maskIP[14], maskIP[15])
			computedNetwork := ip.Mask(mask).String()
			computedNetworks = append(computedNetworks, &computedNetwork)
		}
	}

	node := &model.Node{
		Name:     hostNameAndDomain[0],
		UUID:     host.UUID,
		Iqns:     iqns,
		Networks: computedNetworks,
		Wwpns:    wwpns,
	}

	nodeID, err := driver.flavor.LoadNodeInfo(node)
	if err != nil {
		return "", status.Error(codes.Internal, "Failed to load node info")
	}

	return nodeID, nil

	// NOTE...
	// No secrets are provided to NodeGetInfo.  Therefore, we cannot connect to a CSP here in order to tell it about
	// this node prior to any possible publish request.  We must wait for ControllerPublishVolume to notify the CSP
}

// NodeGetCapabilities ...
//
// A Node Plugin MUST implement this RPC call. This RPC allows the CO to check the supported capabilities of node service provided by the
// Plugin.
// nolint: dupl
func (driver *Driver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	log.Trace(">>>>> NodeGetCapabilities")
	defer log.Trace("<<<<< NodeGetCapabilities")

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: driver.nodeServiceCapabilities,
	}, nil
}

func writeStagedDeviceInfo(targetPath string, stagingDev *StagingDevice) error {
	log.Tracef(">>>>> writeStagedDeviceInfo, targetPath: %s, stagingDev: %+v", targetPath, stagingDev)
	defer log.Trace("<<<<< writeStagedDeviceInfo")

	if stagingDev == nil || stagingDev.Device == nil {
		return fmt.Errorf("Invalid staging device info. Staging device cannot be nil")
	}

	// Encode from device object
	deviceInfo, err := json.Marshal(stagingDev)
	if err != nil {
		return err
	}
	// Write to file
	filename := path.Join(targetPath, deviceInfoFileName)
	err = ioutil.WriteFile(filename, deviceInfo, 0600)
	if err != nil {
		log.Errorf("Failed to write to file %s, err: %v", filename, err.Error())
		return err
	}

	return nil
}

func readStagedDeviceInfo(targetPath string) (*StagingDevice, error) {
	log.Trace(">>>>> readStagedDeviceInfo, targetPath: ", targetPath)
	defer log.Trace("<<<<< readStagedDeviceInfo")

	filePath := path.Join(targetPath, deviceInfoFileName)

	// Check if the file exists
	exists, _, err := util.FileExists(filePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to check if device info file %s exists, %v", filePath, err.Error())
	}
	if !exists {
		return nil, fmt.Errorf("Device info file %s does not exist", filePath)
	}

	// Read from file
	deviceInfo, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Decode into device object
	var stagingDev StagingDevice
	err = json.Unmarshal(deviceInfo, &stagingDev)
	if err != nil {
		return nil, err
	}

	return &stagingDev, nil
}

func removeStagedDeviceInfo(targetPath string) error {
	filePath := path.Join(targetPath, deviceInfoFileName)
	log.Trace(">>>>> removeStagedDeviceInfo, filePath: ", filePath)
	defer log.Trace("<<<<< removeStagedDeviceInfo")
	return util.FileDelete(filePath)
}
