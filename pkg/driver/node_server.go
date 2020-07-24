// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/resource"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/stringformat"
	"github.com/hpe-storage/common-host-libs/util"
)

var (
	stageLock              sync.Mutex
	unstageLock            sync.Mutex
	ephemeralPublishLock   sync.Mutex
	ephemeralUnpublishLock sync.Mutex
)
var isWatcherEnabled bool = false

// Helper utility to construct default mountpoint path
func getDefaultMountPoint(id string) string {
	return fmt.Sprintf("%s/%s", defaultMountDir, id)
}

func getMountInfo(volumeID string, volCap *csi.VolumeCapability, publishContext map[string]string, mountPoint string) *Mount {
	log.Tracef(">>>>> getMountInfo, volumeID: %s, mountPath: %s", volumeID, mountPoint)
	defer log.Trace("<<<<< getMountInfo")

	// Get Mount options from the requested volume capability and read-only flag
	mountOptions := getMountOptionsFromVolCap(volCap)
	// Check if 'Read-Only' is set in the Publish context (By ControllerPublish)
	if publishContext[readOnlyKey] == trueKey {
		log.Trace("Adding'read-only' mount option from the Publish context")
		mountOptions = append(mountOptions, "ro")
	}

	// Read filesystem info from the publish context
	fsOpts := &model.FilesystemOpts{
		Type:       publishContext[fsTypeKey],
		Mode:       publishContext[fsModeKey],
		Owner:      publishContext[fsOwnerKey],
		CreateOpts: publishContext[fsCreateOptionsKey],
	}

	//mountPoint = getDefaultMountPoint(volumeID)

	return &Mount{
		MountPoint:        mountPoint,
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

// removeStaleBindMounts removes the stale bind mount points that are associated with the device
func (driver *Driver) removeStaleBindMounts(device *model.Device, stagingPath string) error {
	log.Tracef(">>>>> removeStaleBindMounts, device: %+v, stagingPath: %s", device, stagingPath)
	defer log.Trace("<<<<< removeStaleBindMounts")

	// Get all mounts for device
	mounts, err := driver.chapiDriver.GetMountsForDevice(device)
	if err != nil {
		return fmt.Errorf("Error retrieving mounts for the device %s", device.AltFullPathName)
	}
	for _, mount := range mounts {
		// Look for stale bind mounts if any and clean up them.
		if mount.Mountpoint != stagingPath {
			log.Warnf("Found stale bindMount %v, Attempting to unmount filesystem from target path", mount.Mountpoint)
			err := driver.chapiDriver.BindUnmount(mount.Mountpoint)
			if err != nil {
				return fmt.Errorf("Error unmounting target path %s, err: %s", mount.Mountpoint, err.Error())
			}
		}
	}
	return nil
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

	// Stage volume at the staging path of the node
	err := driver.nodeStageVolume(
		request.VolumeId,
		request.StagingTargetPath,
		getDefaultMountPoint(request.VolumeId), // Default mount point for staging
		request.VolumeCapability,
		request.Secrets,
		request.PublishContext,
		request.VolumeContext,
	)
	if err != nil {
		return nil, err
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (driver *Driver) nodeStageVolume(
	volumeID string,
	stagingTargetPath string,
	stagingMountPoint string,
	volumeCapability *csi.VolumeCapability,
	secrets map[string]string,
	publishContext map[string]string,
	volumeContext map[string]string) error {

	log.Tracef(">>>>> nodeStageVolume, volume %s, stagingTargetPath: %s, stagingMountPoint: %s, volumeCapability: %v, publishContext: %v, volumeContext: %v",
		volumeID, stagingTargetPath, stagingMountPoint, volumeCapability, publishContext, volumeContext)
	defer log.Trace("<<<<< nodeStageVolume")

	// Validate Volume Capability
	log.Tracef("Validating volume capability: %+v", volumeCapability)
	_, err := driver.IsValidVolumeCapability(volumeCapability)
	if err != nil {
		log.Errorf("Found unsupported volume capability %+v", volumeCapability)
		return err
	}

	// Get volume access type (Block or Mount)
	volAccessType, err := driver.getVolumeAccessType(volumeCapability)
	if err != nil {
		log.Errorf("Failed to retrieve volume access type, err: %v", err.Error())
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to retrieve volume access type, %v", err.Error()))
	}

	// Controller published volume access type must match with the requested volcap
	if publishContext[volumeAccessModeKey] != "" && publishContext[volumeAccessModeKey] != volAccessType.String() {
		log.Errorf("Controller published volume access type %v mismatched with the requested access type %v",
			publishContext[volumeAccessModeKey], volAccessType.String())
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("Controller already published the volume with access type %v, but node staging requested with access type %v",
				publishContext[volumeAccessModeKey], volAccessType.String()))
	}

	log.Infof("NodeStageVolume requested volume %s with access type %s, targetPath %s, capability %v, publishContext %v and volumeContext %v",
		volumeID, volAccessType.String(), stagingTargetPath, volumeCapability, publishContext, volumeContext)

	// Check if volume is requested with NFS resources and intercept here
	if driver.IsNFSResourceRequest(volumeContext) {
		log.Infof("NodeStageVolume requested with NFS resources, returning success")
		return nil
	}

	// Get Volume
	if _, err := driver.GetVolumeByID(volumeID, secrets); err != nil {
		log.Error("Failed to get volume ", volumeID)
		return err // NOT_FOUND
	}

	// Stage the volume on the node by creating a new device with block or mount access.
	// If already staged, then validate it and return appropriate response.
	// Check if the volume has already been staged. If yes, then return here with success
	staged, err := driver.isVolumeStaged(
		volumeID,
		stagingTargetPath,
		stagingMountPoint,
		volAccessType,
		volumeCapability,
		secrets,
		publishContext,
		volumeContext,
	)
	if err != nil {
		log.Errorf("Error validating the staged info for volume %s, err: %v", volumeID, err.Error())
		return err
	}
	if staged {
		log.Infof("Volume %s has already been staged. Returning here", volumeID)
		return nil // volume already staged, do nothing and return here
	}

	// Stage volume - Create device and expose volume as raw block or mounted directory (filesystem)
	log.Tracef("NodeStageVolume staging volume %s to the staging path %s", volumeID, stagingTargetPath)
	stagingDev, err := driver.stageVolume(
		volumeID,
		stagingMountPoint,
		volAccessType,
		volumeCapability,
		publishContext,
		volumeContext,
	)
	if err != nil {
		return status.Error(codes.Internal,
			fmt.Sprintf("Failed to stage volume %s, err: %s", volumeID, err.Error()))
	}
	log.Tracef("Staged volume %s successfully, StagingDev: %#v", volumeID, stagingDev)

	// Save staged device info in the staging area
	log.Tracef("NodeStageVolume writing device info %+v to staging target path %s", stagingDev.Device, stagingTargetPath)
	if stagingDev == nil || stagingDev.Device == nil {
		return fmt.Errorf("Invalid staging device info. Staging device cannot be nil")
	}
	err = writeData(stagingTargetPath, deviceInfoFileName, stagingDev)
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("Failed to stage volume %s, err: %s", volumeID, err.Error()))
	}
	return nil
}

// isVolumeStaged checks if the volume is already been staged on the node.
// If already staged, then returns true else false
func (driver *Driver) isVolumeStaged(
	volumeID string,
	stagingTargetPath string,
	stagingMountPoint string,
	volAccessType model.VolumeAccessType,
	volumeCapability *csi.VolumeCapability,
	secrets map[string]string,
	publishContext map[string]string,
	volumeContext map[string]string) (bool, error) {

	//request *csi.NodeStageVolumeRequest, volAccessType model.VolumeAccessType) (bool, error) {
	log.Tracef(">>>>> isVolumeStaged, volumeID: %s, stagingTargetPath: %s, stagingMountPoint: %s, volAccessType: %s",
		volumeID, stagingTargetPath, stagingMountPoint, volAccessType.String())
	defer log.Trace("<<<<< isVolumeStaged")

	// Check if the staged device file exists
	filePath := path.Join(stagingTargetPath, deviceInfoFileName)
	exists, _, _ := util.FileExists(filePath)
	if !exists {
		return false, nil // Not staged as file doesn't exist
	}

	// Read the device info from the staging path
	stagingDev, _ := readStagedDeviceInfo(stagingTargetPath)
	if stagingDev == nil {
		return false, nil // Not staged as device info not found
	}
	log.Tracef("Found staged device: %+v", stagingDev)

	// Check if the volume ID matches with the device info (already staged)
	if volumeID != stagingDev.VolumeID {
		log.Errorf("Volume %s is not matching with the staged volume ID %s",
			volumeID, stagingDev.VolumeID)
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
			return false, nil // Not staged yet as targetPath does not exists
		}
	}

	// Validate the requested mount details with the staged device details
	if volAccessType == model.MountType && stagingDev.MountInfo != nil {
		mountInfo := getMountInfo(volumeID, volumeCapability, publishContext, stagingMountPoint)

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
					volumeID, stagingTargetPath))
		}
		// TODO: Check for fsOpts compatibility
	}

	return true, nil
}

// stageVolume performs all the necessary tasks to stage the volume on the node to the staging path
func (driver *Driver) stageVolume(
	volumeID string,
	stagingMountPoint string,
	volAccessType model.VolumeAccessType,
	volCap *csi.VolumeCapability,
	publishContext map[string]string,
	volumeContext map[string]string) (*StagingDevice, error) {

	log.Tracef(">>>>> stageVolume, volumeID: %s, stagingMountPoint: %s, volumeAccessType: %v, volCap: %v, publishContext: %v, volumeContext: %v",
		volumeID, stagingMountPoint, volAccessType.String(), volCap, publishContext, volumeContext)
	defer log.Trace("<<<<< stageVolume")

	// serialize stage requests
	stageLock.Lock()
	defer stageLock.Unlock()

	// Create device for volume on the node
	device, err := driver.setupDevice(publishContext)
	if err != nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("Error creating device for volume %s, err: %v", volumeID, err.Error()))
	}
	log.Infof("Device setup successful, Device: %+v", device)

	// Construct staging device to be stored in the staging path on the node
	stagingDevice := &StagingDevice{
		VolumeID:         volumeID,
		VolumeAccessMode: volAccessType,
		Device:           device,
	}

	// If Block, then stage the volume for raw block device access
	if volAccessType == model.BlockType {
		// Do nothing
		return stagingDevice, nil
	}

	// Else Mount, then stage the volume for filesystem access
	// Get mount info from the request
	mountInfo := getMountInfo(volumeID, volCap, publishContext, stagingMountPoint)

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

	discoveryIps := strings.Split(publishContext[discoveryIPsKey], ",")
	iqns := strings.Split(publishContext[targetNamesKey], ",")

	volume := &model.Volume{
		SerialNumber:   publishContext[serialNumberKey],
		AccessProtocol: publishContext[accessProtocolKey],
		Iqns:           iqns,
		TargetScope:    publishContext[targetScopeKey],
		LunID:          publishContext[lunIDKey],
		DiscoveryIPs:   discoveryIps,
		ConnectionMode: defaultConnectionMode,
		SecondaryArrayDetails: publishContext[secondaryArrayDetailsKey],
	}
	if publishContext[accessProtocolKey] == iscsi {
		chapInfo := &model.ChapInfo{
			Name:     publishContext[chapUsernameKey],
			Password: publishContext[chapPasswordKey],
		}
		volume.Chap = chapInfo
	}

	// Cleanup any stale device existing before stage
	device, err := driver.chapiDriver.GetDevice(volume)
	if device != nil {
		device.TargetScope = volume.TargetScope
		err = driver.chapiDriver.DeleteDevice(device)
		if err != nil {
			log.Warnf("Failed to cleanup stale device %s before staging, err %s", device.AltFullPathName, err.Error())
		}
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
	if scope, ok := publishContext[targetScopeKey]; ok {
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

	// Unstage the volume from the staging area
	log.Infof("NodeUnStageVolume requested volume %s with targetPath %s", request.VolumeId, request.StagingTargetPath)
	if err := driver.nodeUnstageVolume(request.VolumeId, request.StagingTargetPath); err != nil {
		log.Errorf("Failed to unstage volume %s, err: %s", request.VolumeId, err.Error())
		return nil, err
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (driver *Driver) nodeUnstageVolume(volumeID string, stagingTargetPath string) error {
	log.Tracef(">>>>> nodeUnstageVolume, volumeID: %s, stagingTargetPath: %s", volumeID, stagingTargetPath)
	defer log.Trace("<<<<< nodeUnstageVolume")

	// Check if the staged device file exists
	deviceFilePath := path.Join(stagingTargetPath, deviceInfoFileName)
	exists, _, _ := util.FileExists(deviceFilePath)
	if !exists {
		log.Infof("Volume %s not in staged state as the device info file %s does not exist. Returning here",
			volumeID, deviceFilePath)
		return nil // Already unstaged as device file doesn't exist
	}

	// Read the device info from the staging path if exists
	stagingDev, _ := readStagedDeviceInfo(stagingTargetPath)
	if stagingDev == nil {
		log.Infof("Volume %s not in staged state as the staging device info does not exist. Returning here", volumeID)
		return nil // Already unstaged as device info doesn't exist
	}
	log.Tracef("Found staged device info: %+v", stagingDev)

	device := stagingDev.Device
	if device == nil {
		return status.Error(codes.Internal,
			fmt.Sprintf("Missing device info in the staging device %v", stagingDev))
	}

	// serialize unstage requests
	unstageLock.Lock()
	defer unstageLock.Unlock()

	// If mounted, then unmount the filesystem
	if stagingDev.VolumeAccessMode == model.MountType && stagingDev.MountInfo != nil {
		// Remove the stale bindMounts if any
		if err := driver.removeStaleBindMounts(stagingDev.Device, stagingDev.MountInfo.MountPoint); err != nil {
			return status.Error(codes.Internal,
				fmt.Sprintf("Error while deleting the stale bind mounts for the staged device %v, err: %s", stagingDev, err.Error()))
		}

		// Unmount the device from the mountpoint
		_, err := driver.chapiDriver.UnmountDevice(stagingDev.Device, stagingDev.MountInfo.MountPoint)
		if err != nil {
			log.Errorf("Failed to unmount device %s from mountpoint %s, err: %s",
				device.AltFullPathName, stagingDev.MountInfo.MountPoint, err.Error())
			return status.Error(codes.Internal,
				fmt.Sprintf("Error unmounting device %s from mountpoint %s, err: %s",
					device.AltFullPathName, stagingDev.MountInfo.MountPoint, err.Error()))
		}
	}

	// Delete device
	log.Tracef("NodeUnstageVolume deleting device %+v", device)
	if err := driver.chapiDriver.DeleteDevice(device); err != nil {
		log.Errorf("Failed to delete device with path name %s.  Error: %s", device.Pathname, err.Error())
		return status.Error(codes.Internal, "Error deleting device "+device.Pathname)
	}
	// Remove the device file
	removeDataFile(stagingTargetPath, deviceInfoFileName)
	return nil
}

// Returns true if the ephemeral is set to true, else returns false
func isEphemeral(volContext map[string]string) bool {
	return volContext[csiEphemeral] == trueKey
}

// Returns volume name with ephemeral prefix
func getEphemeralVolName(podName string, volumeHandle string) string {
	// Format: ephemeral-<PodName>-<VolumeHandle>
	podNameInfix := podName
	podNameLength := len(podNameInfix)
	trimSize := 32
	if podNameLength > trimSize {
		log.Infof("Truncating the actual podName '%s' infix from %d to %d chars", podName, podNameLength, trimSize)
		podNameInfix = podNameInfix[podNameLength-trimSize:] // Trim the podName to retain only the last 'n' chars
	}
	return fmt.Sprintf("%s-%s-%s", ephemeralKey, podNameInfix, volumeHandle)
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
	log.Tracef(">>>>> NodePublishVolume, VolumeId: %s, stagingPath: %s, targetStagingPath: %s",
		request.VolumeId, request.TargetPath, request.StagingTargetPath)
	defer log.Trace("<<<<< NodePublishVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume ID specified for NodePublishVolume")
	}

	if request.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Invalid target path specified for NodePublishVolume")
	}

	// Check if ephemeral volume request
	ephemeral := isEphemeral(request.GetVolumeContext())

	// Ephemeral volume request does not contain staging path. so skip this validation check.
	if !ephemeral && request.StagingTargetPath == "" {
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

	// Ephemeral volume request does not contain 'publishContext'. So, skip this validation.
	if !ephemeral {
		// Controller published volume access type must match with the requested volcap
		if request.PublishContext[volumeAccessModeKey] != "" && request.PublishContext[volumeAccessModeKey] != volAccessType.String() {
			log.Errorf("Controller published volume access type '%v' mismatched with the requested access type '%v'",
				request.PublishContext[volumeAccessModeKey], volAccessType.String())
			return nil, status.Error(codes.InvalidArgument,
				fmt.Sprintf("Controller already published the volume with access type %v, but node publish requested with access type %v",
					request.PublishContext[volumeAccessModeKey], volAccessType.String()))
		}
	}

	log.Infof("NodePublishVolume requested volume %s with access type %s, targetPath %s, capability %v, publishContext %v and volumeContext %v",
		request.VolumeId, volAccessType, request.TargetPath, request.VolumeCapability, request.PublishContext, request.VolumeContext)

	// Check if volume is requested with NFS resources and intercept here
	if driver.IsNFSResourceRequest(request.VolumeContext) {
		log.Infof("NodePublish requested with NFS resources for %s", request.VolumeId)
		return driver.flavor.HandleNFSNodePublish(request)
	}

	// If ephemeral volume request, then create new volume, add ACL and NodeStage/NodePublish
	if ephemeral {
		// Handle inline ephemeral volume request
		podName := request.VolumeContext[csiEphemeralPodName]
		log.Tracef("Processing request for ephemeral volume %s for pod %s with access type %s",
			request.VolumeId, podName, volAccessType.String())

		// For ephemeral volume, targetPath's parent directory will be used as 'stagingTargetPath'
		// The device info file will be placed in this directory.
		stagingTargetPath := filepath.Dir(request.TargetPath)

		// Construct ephemeral volume name from the given volume handle and podName
		volumeName := getEphemeralVolName(
			podName,          // POD Name
			request.VolumeId, // Ephemeral Volume Handle
		)
		err := driver.nodePublishEphemeralVolume(
			request.VolumeId, // Ephemeral CSI Volume Handle
			volumeName,
			stagingTargetPath,
			request.TargetPath,
			request.VolumeCapability,
			request.Secrets,
			request.Readonly,
			request.PublishContext,
			request.VolumeContext,
		)
		if err != nil {
			return nil, err
		}
		log.Infof("Successfully published the ephemeral volume %s with name %s to the target path %s",
			request.VolumeId, volumeName, request.TargetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Node publish
	err = driver.nodePublishVolume(
		request.VolumeId,
		request.StagingTargetPath,
		request.TargetPath,
		request.VolumeCapability,
		request.Secrets,
		request.Readonly,
		request.PublishContext,
		request.VolumeContext,
	)
	if err != nil {
		log.Errorf("Failed to node publish volume %s, err: %s", request.VolumeId, err.Error())
		return nil, err
	}

	log.Infof("Successfully published the volume %s to the target path %s",
		request.VolumeId, request.TargetPath)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (driver *Driver) nodePublishVolume(
	volumeID string,
	stagingTargetPath string,
	targetPath string,
	volumeCapability *csi.VolumeCapability,
	secrets map[string]string,
	readOnly bool,
	publishContext map[string]string,
	volumeContext map[string]string) error {

	log.Tracef(">>>>> nodePublishVolume, volumeID: %s, stagingTargetPath: %s, targetPath: %s",
		volumeID, stagingTargetPath, targetPath)
	defer log.Trace("<<<<< nodePublishVolume")

	// Get Volume
	if _, err := driver.GetVolumeByID(volumeID, secrets); err != nil {
		log.Error("Failed to get volume ", volumeID)
		return status.Error(codes.NotFound, err.Error())
	}

	// Read device info from the staging area
	stagingDev, err := readStagedDeviceInfo(stagingTargetPath)
	if err != nil {
		return status.Error(codes.FailedPrecondition,
			fmt.Sprintf("Staging target path %s not set, err: %s", stagingTargetPath, err.Error()))
	}
	if stagingDev == nil {
		return status.Error(codes.FailedPrecondition,
			fmt.Sprintf("Staging device is not configured at the staging path %s", stagingTargetPath))
	}

	// Check if the volume has already published on the targetPath.
	// If published, then return success, else perform bind-mount operation.
	published, err := driver.isVolumePublished(
		volumeID,
		targetPath,
		volumeCapability,
		publishContext,
		stagingDev,
	)
	if err != nil {
		log.Errorf("Error while validating the published info for volume %s, err: %v",
			volumeID, err.Error())
		return err
	}
	if published {
		log.Infof("The target path %s has already been published for volume %s. Returning here",
			targetPath, volumeID)
		return nil // VOLUME TARGET ALREADY PUBLISHED
	}

	// If Block, then stage the volume for raw block device access
	if publishContext[volumeAccessModeKey] == model.BlockType.String() {
		log.Tracef("Publishing the volume for raw block access (create symlink), devicePath: %s, targetPath: %v",
			stagingDev.Device.AltFullPathName, targetPath)

		// Check if target path symlink to the device already exists
		exists, symlink, _ := util.IsFileSymlink(targetPath)
		if symlink {
			errMsg := fmt.Sprintf("Target path %s already published as symlink to the device %s", targetPath, stagingDev.Device.AltFullPathName)
			log.Error("Error: ", errMsg)
			return status.Error(codes.Internal, errMsg)
		}
		if exists {
			// Remove the target path before creating the symlink
			log.Tracef("Removing the target path %s before creating symlink to the device", targetPath)
			if err := util.FileDelete(targetPath); err != nil {
				return status.Error(codes.Internal,
					fmt.Sprintf("Error removing the target path %s before creating symlink to the device, err: %s",
						targetPath, err.Error()))
			}
		}

		// Note: Bind-mount is not allowed for raw block device as there is no filesystem on it.
		//       So, we create softlink to the device file. TODO: mknode() instead ???
		//       Ex: ln -s /dev/mpathbm <targetPath>
		if err := os.Symlink(stagingDev.Device.AltFullPathName, targetPath); err != nil {
			errMsg := fmt.Sprintf("Failed to create symlink %s to the device path %s, err: %v",
				targetPath, stagingDev.Device.AltFullPathName, err.Error())
			log.Error("Error: ", errMsg)
			return status.Error(codes.Internal, errMsg)
		}
	} else {
		log.Tracef("Publishing the volume for filesystem access, stagedPath: %s, targetPath: %v",
			stagingDev.MountInfo.MountPoint, targetPath)
		// Publish: Bind mount the staged mountpoint on the target path
		if err = driver.chapiDriver.BindMount(stagingDev.MountInfo.MountPoint, targetPath, false); err != nil {
			return status.Error(codes.Internal,
				fmt.Sprintf("Failed to publish volume %s to the target path %s, err: %v",
					volumeID, targetPath, err.Error()))
		}
	}
	return nil
}

// retry the rollback on failure
func (driver *Driver) retryRollbackEphemeralVolume(
	volumeHandle string,
	volumeName string,
	secrets map[string]string,
	stagingTargetPath string,
	targetPath string) error {

	try := 0
	maxTries := 10
	for {
		err := driver.rollbackEphemeralVolume(
			volumeHandle,
			volumeName,
			secrets,
			stagingTargetPath,
			targetPath,
		)
		if err != nil {
			if try < maxTries {
				try++
				log.Tracef("Retry attempt %d unsuccessful. Pending retries %d", try, maxTries-try)
				time.Sleep(time.Duration(try) * time.Second)
				continue
			}
			log.Errorf("Unable to rollback ephemeral volume %s after %d retries", volumeName, maxTries)

			// Destroy the volume
			log.Infof("Attempting to destroy ephemeral volume %s", volumeName)
			delErr := driver.DeleteVolumeByName(volumeName, secrets, true)
			if delErr != nil {
				log.Errorf("Unable to destroy ephemeral volume %s from the backend via CSP, err: %s",
					volumeName, err.Error())
				// Not returning the delete error here
			}
			return err
		}
		return nil
	}
}

func (driver *Driver) rollbackEphemeralVolume(
	volumeHandle string,
	volumeName string,
	secrets map[string]string,
	stagingTargetPath string,
	targetPath string) error {

	log.Tracef(">>>>> rollbackEphemeralVolume, volumeID: %s, stagingTargetPath: %s, targetPath: %s",
		volumeHandle, stagingTargetPath, targetPath)
	defer log.Trace("<<<<< rollbackEphemeralVolume")

	// Node Unpublish node (umount)
	err := driver.nodeUnpublishVolume(targetPath)
	if err != nil {
		log.Errorf("Error node unpublishing the volume %s, err: %s", volumeHandle, err.Error())
		return err
	}
	log.Tracef("Successfully node unpublished volume %s from targetPath %s", volumeHandle, targetPath)

	// Node Unstage (remove device and staging unmount)
	err = driver.nodeUnstageVolume(volumeHandle, stagingTargetPath)
	if err != nil {
		log.Errorf("Error node unstaging the volume %s, err: %s", volumeHandle, err.Error())
		return err
	}
	log.Tracef("Successfully node unstaged volume %s from targetPath %s", volumeHandle, targetPath)

	// Get the volume if exists
	volume, err := driver.GetVolumeByName(volumeName, secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return err
	}

	// Remove ACL and then destroy volume
	if volume != nil {
		// Get node info
		nodeID, err := driver.nodeGetInfo()
		if err != nil {
			log.Errorf("err: %s", err.Error())
			return err
		}
		// Unpublish controller for the nodeID
		err = driver.controllerUnpublishVolume(volume.ID, nodeID, secrets)
		if err != nil {
			log.Errorf("Error controller unpublishing the volume %s, err: %s", volume.ID, err.Error())
			return err
		}
		log.Tracef("Successfully controller unpublished volume %s from targetPath %s", volumeHandle, targetPath)

		// Destroy volume permanently from the backend
		log.Infof("Destroying the volume %s with id %s", volumeName, volume.ID)
		err = driver.deleteVolume(volume.ID, secrets, true)
		if err != nil {
			log.Errorf("Error destroying the volume %s with ID %s, err: %s",
				volumeName, volume.ID, err.Error())
			return err
		}
		log.Tracef("Successfully destroyed volume %s with id %s from targetPath %s", volumeName, volume.ID, targetPath)
	}
	// Remove ephemeral data file as volume does not exists anymore
	removeDataFile(stagingTargetPath, ephemeralDataFileName)

	// Delete DB entry
	if err := driver.RemoveFromDB(volume.Name); err != nil {
		return err
	}
	return nil
}

func (driver *Driver) getEphemeralVolumeSecret(volumeHandle string, volumeContext map[string]string) (map[string]string, *Secret, error) {
	log.Tracef(">>>>> getEphemeralVolumeSecret, volumeHandle: %s, volumeContext: %v", volumeHandle, volumeContext)
	defer log.Trace("<<<<< getEphemeralVolumeSecret")

	var secretRef *Secret
	// Get the secret reference from volume context if specified
	if volumeContext[secretNameKey] != "" && volumeContext[secretNamespaceKey] != "" {
		log.Tracef("Extract the secret reference from volume context for volume %s", volumeHandle)
		secretRef = &Secret{
			Name:      volumeContext[secretNameKey],
			Namespace: volumeContext[secretNamespaceKey],
		}
	} else {
		log.Tracef("Fetch the secret reference from POD for volume %s", volumeHandle)
		// POD name and namespace
		podName := volumeContext[csiEphemeralPodName]
		podNamespace := volumeContext[csiEphemeralPodNamespace]

		volSecretName, err := driver.flavor.GetEphemeralVolumeSecretFromPod(volumeHandle, podName, podNamespace)
		if err != nil {
			return nil, nil, status.Error(codes.InvalidArgument,
				fmt.Sprintf("Failed to fetch secret for ephemeral volume %s from pod %s/%s, %s",
					volumeHandle, podName, podNamespace, err.Error()))
		}
		secretRef = &Secret{
			Name:      volSecretName,
			Namespace: podNamespace,
		}
	}
	secrets, err := driver.flavor.GetCredentialsFromSecret(secretRef.Name, secretRef.Namespace)
	if err != nil {
		log.Errorf("Failed to fetch credentials from secret %s/%s for ephemeral volume %s, %s",
			secretRef.Name, secretRef.Namespace, volumeHandle, err.Error())
		return nil, nil, err
	}
	return secrets, secretRef, nil
}

func (driver *Driver) nodePublishEphemeralVolume(
	volumeHandle string,
	volumeName string,
	stagingTargetPath string,
	targetPath string,
	volumeCapability *csi.VolumeCapability,
	secrets map[string]string,
	readOnly bool,
	publishContext map[string]string,
	volumeContext map[string]string) error {

	log.Tracef(">>>>> nodePublishEphemeralVolume, volumeHandle: %s, targetPath: %s, volumeCapability: %v, readOnly: %v, volumeContext: %v, publishContext: %v",
		volumeHandle, targetPath, volumeCapability, readOnly, volumeContext, publishContext)
	defer log.Trace("<<<<< nodePublishEphemeralVolume")

	// serialize inline ephemeral volume NodePublish() requests
	ephemeralPublishLock.Lock()
	defer ephemeralPublishLock.Unlock()

	podUID := volumeContext[csiEphemeralPodUID]
	// POD UID must be provided in the request
	if podUID == "" {
		log.Errorf("Missing POD uid in the volume context for volume %s", volumeName)
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("NodePublish of ephemeral volume %s failed due to missing POD uid in the request", volumeHandle))
	}

	// Get Secret
	secrets, secretRef, err := driver.getEphemeralVolumeSecret(volumeHandle, volumeContext)
	if err != nil {
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to node publish ephemeral volume %s, %s", volumeHandle, err.Error()))
	}

	// Create and Publish inline ephemeral volume
	log.Infof("Provisioning ephemeral volume %s with name %s for pod %s", volumeHandle, volumeName, podUID)
	err = driver.createPublishEphemeralVolume(
		volumeHandle,
		volumeName,
		stagingTargetPath,
		targetPath,
		volumeCapability,
		secrets,
		secretRef,
		readOnly,
		publishContext,
		volumeContext,
	)
	if err != nil {
		log.Errorf("Failed to node publish ephemeral volume %s, attempting to rollback, err: %s", volumeName, err.Error())
		if status.Code(err) == codes.AlreadyExists {
			log.Errorf("Failed to rollback ephemeral volume %s, err: %s", volumeName, err.Error())
			return err
		}
		// Attempt rollback
		rbErr := driver.retryRollbackEphemeralVolume(
			volumeHandle,
			volumeName,
			secrets,
			stagingTargetPath,
			targetPath,
		)
		if rbErr != nil {
			log.Errorf("Failed to rollback ephemeral volume %s, err: %s", volumeName, rbErr.Error())
			// Returning both original error + rollback error
			return status.Error(codes.Internal,
				fmt.Sprintf("%s, %s", err.Error(), rbErr.Error()))
		}
		log.Infof("Rollback successful for ephemeral volume %s", volumeName)
		return err
	}
	return nil
}

func (driver *Driver) createPublishEphemeralVolume(
	volumeHandle string,
	volumeName string,
	stagingTargetPath string,
	targetPath string,
	volumeCapability *csi.VolumeCapability,
	secrets map[string]string,
	secretRef *Secret,
	readOnly bool,
	publishContext map[string]string,
	volumeContext map[string]string) error {

	log.Tracef(">>>>> createPublishEphemeralVolume, volumeName: %s, stagingTargetPath: %s, targetPath: %s",
		volumeName, stagingTargetPath, targetPath)
	defer log.Trace("<<<<< createPublishEphemeralVolume")

	// Do the following for ephemeral inline volume:
	// 		1) Create new volume using volume context parameters
	// 		2) Controller publish volume (Add ACL)
	// 		3) Stage volume on the node
	//			-	Create device
	//			- 	Mount to the target path using mount options and read-only flag)
	//			-	Persist the staged device info within the POD directory path (targetPath's parent directory).
	//		4) Publish volume on the node (Validates if device is mounted on the targetPath)

	// Create new inline volume
	volume, err := driver.createEphemeralVolume(
		volumeHandle,
		volumeName,
		stagingTargetPath,
		volumeCapability,
		secrets,
		secretRef,
		volumeContext,
	)
	if err != nil {
		log.Errorf("Failed to provision ephemeral volume %s with name %s, %s", volumeHandle, volumeName, err.Error())
		return err
	}

	// Get Node Info
	var nodeID string
	nodeID, err = driver.nodeGetInfo()
	if err != nil {
		log.Tracef("Failed to get node info, err: %s", err.Error())
		return status.Error(codes.Internal,
			fmt.Sprintf("Failed to node publish ephemeral volume %s with id %s, err: %s",
				volumeName, volume.ID, err.Error()))
	}

	// Controller publish volume by adding ACL
	publishContext, err = driver.controllerPublishVolume(
		volume.ID,
		nodeID,
		secrets,
		volumeCapability,
		readOnly,
		volumeContext,
	)
	if err != nil {
		log.Errorf("Error controller publishing volume %s (id: %s), err: %s",
			volumeName, volume.ID, err.Error())
		return err
	}
	log.Infof("Successfully controller published ephemeral volume %s (id: %s) with publishContext %+v",
		volumeName, volume.ID, publishContext)

	// Node stage volume (Publish to Node)
	err = driver.nodeStageVolume(
		volume.ID,
		stagingTargetPath,
		targetPath, // Mountpoint
		volumeCapability,
		secrets,
		publishContext,
		volumeContext,
	)
	if err != nil {
		log.Errorf("Failed to node stage volume %s (id: %s), err: %s",
			volumeName, volume.ID, err.Error())
		return err
	}
	log.Infof("Successfully node staged ephemeral volume %s (id: %s) on stagingTargetPath %s",
		volumeName, volume.ID, stagingTargetPath)

	// Node publish volume. This will ensure that the ephemeral volume is published to the node.
	err = driver.nodePublishVolume(
		volume.ID,
		stagingTargetPath,
		targetPath,
		volumeCapability,
		secrets,
		readOnly,
		publishContext,
		volumeContext,
	)
	if err != nil {
		log.Errorf("Failed to node publish volume %s (id: %s), err: %s",
			volumeName, volume.ID, err.Error())
		return err
	}
	log.Infof("Successfully node published ephemeral volume %s (id: %s) on targetPath %s",
		volumeName, volume.ID, targetPath)
	return nil
}

// isVolumePublished returns true if the volume is already published, else returns false
func (driver *Driver) isVolumePublished(
	volumeID string,
	targetPath string,
	volumeCapability *csi.VolumeCapability,
	publishContext map[string]string,
	stagingDev *StagingDevice) (bool, error) {

	log.Tracef(">>>>> isVolumePublished, volumeID: %s, targetPath: %s, volumeCapability: %v, publishContext: %v, stagingDev: %+v",
		volumeID, targetPath, volumeCapability, publishContext, stagingDev)
	defer log.Trace("<<<<< isVolumePublished")

	// Check for volume ID match with the staged device info
	if stagingDev.VolumeID != volumeID {
		return false, nil // Not published yet as volume id mismatch
	}
	if stagingDev.Device == nil {
		return false, nil // Not published yet as device info not found
	}

	// If Block, then check if the target path exists on the node
	if stagingDev.VolumeAccessMode == model.BlockType {
		// Check if the device already published (softlink) to the target path
		log.Tracef("Checking if the device %s already published to the target path %s",
			stagingDev.Device.AltFullPathName, targetPath)
		// Check if path exists
		exists, _, _ := util.FileExists(targetPath)
		if !exists {
			log.Tracef("Target path %s does not exist on the node", targetPath)
			return false, nil // Not published yet as targetPath does not exists
		}

		// Check if target path is the symlink to the device
		_, symlink, _ := util.IsFileSymlink(targetPath)
		if !symlink {
			log.Tracef("Target path %s is not symlink to the device %s", targetPath, stagingDev.Device.AltFullPathName)
			return false, nil // Not published yet as symlink does not exists
		}

		log.Tracef("Target path %s exists on the node", targetPath)
		return true, nil // Published
	}

	// Else Mount:
	// Check if the volume already published (bind-mounted) to the target path
	log.Tracef("Checking if the volume %s already bind-mounted to the target path %s",
		volumeID, targetPath)
	mounted, err := driver.isMounted(stagingDev.Device, targetPath)
	if err != nil {
		log.Errorf("Error while checking if device %s is bind-mounted to target path %s, Err: %s",
			stagingDev.Device.AltFullPathName, targetPath, err.Error())
		return false, err
	}
	if !mounted {
		log.Tracef("Mountpoint %s is not bind-mounted yet", targetPath)
		return false, nil // Not published yet as bind-mount path does not exist
	}

	// If mount access type, then validate the requested mount details with the staged device details
	if stagingDev.MountInfo != nil {
		// Get mount info
		mountInfo := getMountInfo(volumeID, volumeCapability, publishContext, targetPath)

		// Check if reqMountOptions are compatible with staged device's mount options
		log.Tracef("Checking for mount options compatibility: staged options: %v, reqMountOptions: %v",
			stagingDev.MountInfo.MountOptions, mountInfo.MountOptions)
		if !stringformat.StringsLookup(stagingDev.MountInfo.MountOptions, mountInfo.MountOptions) {
			// This means staging has not mounted the device with appropriate mount options for the PVC
			log.Errorf("Mount flags %v are not compatible with the staged mount options %v",
				mountInfo.MountOptions, stagingDev.MountInfo.MountOptions)
			return false, status.Error(codes.AlreadyExists,
				fmt.Sprintf("Volume %s has already been published at the specified staging target_path %s but is incompatible with the specified volume_capability and readonly flag",
					volumeID, targetPath))
		}
		// TODO: Check for fsOpts compatibility
	}
	return true, nil // Published
}

// If not exists, create a new volume.
// If already exists, then it must match with the ephemeral data file contents. If mismatch, then force recreate.
func (driver *Driver) createEphemeralVolume(
	volumeHandle string,
	volumeName string,
	stagingTargetPath string,
	volumeCapability *csi.VolumeCapability,
	secrets map[string]string,
	secretRef *Secret,
	volumeContext map[string]string) (*model.Volume, error) {

	log.Tracef(">>>>> createEphemeralVolume, volumeName: %s, stagingTargetPath: %s", volumeName, stagingTargetPath)
	defer log.Trace("<<<<< createEphemeralVolume")

	// Create DB entry
	dbKey := volumeName
	if err := driver.AddToDB(dbKey, Pending); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer driver.RemoveFromDBIfPending(dbKey)

	// Volume size (Default value will be used if 'sizeInGiB' parameter is unspecified)
	sizeInBytes := defaultVolumeSize

	// Get the volume size from the volume context if specified
	sizeStr := volumeContext[sizeKey]
	if sizeStr != "" {
		volSize := resource.MustParse(sizeStr)
		sizeInBytes = volSize.Value()
		log.Infof("Ephemeral volume %s requested with size %s (%v)", volumeName, sizeStr, sizeInBytes)
	}

	// Construct volume capabitilities to pass to createVolume()
	volCapabilities := []*csi.VolumeCapability{
		volumeCapability,
	}

	// POD information
	podData := &POD{
		UID:       volumeContext[csiEphemeralPodUID],
		Name:      volumeContext[csiEphemeralPodName],
		Namespace: volumeContext[csiEphemeralPodNamespace],
	}

	// Check if the volume with same name already exists
	existingVolume, err := driver.GetVolumeByName(volumeName, secrets)
	if err != nil {
		log.Errorf("Error while trying to check if ephemeral volume %s already exists, err: %s", volumeName, err.Error())
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("NodePublish of ephemeral volume %s failed, %s", volumeName, err.Error()))
	}
	if existingVolume != nil {
		// If volume already exists, then match the ephemeral data from the staging path
		ephemeralData, err := loadEphemeralData(stagingTargetPath, ephemeralDataFileName)
		if err != nil {
			return nil, status.Error(codes.Internal,
				fmt.Sprintf("Failed to load ephemeral data from staging path %s/%s for volume %s, %s",
					stagingTargetPath, ephemeralDataFileName, volumeHandle, err.Error()))
		}
		if ephemeralData != nil {
			// Check if the ephemeralData matches with the requested volumeHandle
			if ephemeralData.VolumeHandle == volumeHandle && ephemeralData.PodData.UID == podData.UID {
				log.Infof("Volume %s already provisioned and the ephemeral data file exists, returning here", volumeName)
				return existingVolume, nil // Already created and ephemeral data file is updated.
			}
		}
		// Force destroy the pre-existing volume and continue to re-create new volume.
		log.Infof("Volume with name %s already exists without ephemeral data file, attempting to force destroy", volumeName)
		err = driver.deleteVolume(existingVolume.ID, secrets, true)
		if err != nil {
			return nil, status.Error(codes.AlreadyExists,
				fmt.Sprintf("Failed to provision ephemeral volume %s, unable to destroy pre-existing volume with id %s, %s",
					volumeName, existingVolume.ID, err.Error()))
		}
		log.Infof("Successfully destroyed the pre-existing ephemeral volume %s with id %s", volumeName, existingVolume.ID)
	}

	// Create a new volume
	csiVolume, err := driver.createVolume(
		volumeName,
		sizeInBytes,
		volCapabilities,
		secrets,
		nil, /* No Volume Source */
		volumeContext,
	)
	if err != nil {
		log.Errorf("Failed to create ephemeral volume %s with size %d, err: %s", volumeName, sizeInBytes, err.Error())
		return nil, status.Error(codes.AlreadyExists,
			fmt.Sprintf("Failed to create ephemeral volume %s with size %d, err: %s", volumeName, sizeInBytes, err.Error()))
	}
	log.Infof("Successfully created ephemeral volume %s with id %s and size %d", volumeName, csiVolume.VolumeId, sizeInBytes)

	// Store ephemeral data for the newly created volume in the staging path
	ephemeralData := Ephemeral{
		VolumeHandle: volumeHandle,
		VolumeID:     csiVolume.VolumeId,
		PodData:      podData,
		SecretRef:    secretRef,
	}

	// persist ephemeral data
	if err := writeData(stagingTargetPath, ephemeralDataFileName, ephemeralData); err != nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("failed to save ephemeral data: %v", err.Error()))
	}
	log.Tracef("Ephemeral data file [%s/%s] created successfully", stagingTargetPath, ephemeralDataFileName)

	// Get Volume using secrets
	volume, err := driver.GetVolumeByID(csiVolume.VolumeId, secrets)
	if err != nil {
		log.Errorf("Failed to get volume %s with id %s, err: %s", volumeName, csiVolume.VolumeId, err.Error())
		return nil, err
	}
	// Update DB entry
	if err := driver.UpdateDB(dbKey, volume); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return volume, nil
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
	log.Tracef(">>>>> NodeUnpublishVolume, VolumeId: %s, TargetPath: %s", request.VolumeId, request.TargetPath)
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
		ephemeral, err := driver.isEphemeralTargetPath(request.TargetPath)
		if err != nil {
			return nil, err
		}
		if ephemeral {
			if err := driver.nodeUnpublishEphemeralVolume(request.VolumeId, request.TargetPath); err != nil {
				log.Errorf("Failed to node unpublish ephemeral volume %s, err: %s", request.VolumeId, err.Error())
				return nil, err
			}
		} else {
			if err := driver.nodeUnpublishVolume(request.TargetPath); err != nil {
				log.Errorf("Failed to node unpublish volume %s, err: %s", request.VolumeId, err.Error())
				return nil, err
			}
		}
	}
	log.Infof("Successfully node unpublished volume %s from targetPath %s", request.VolumeId, request.TargetPath)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// Return true, if device file exists in the targetPath's parent directory, else returns false
func (driver *Driver) isEphemeralTargetPath(targetPath string) (bool, error) {
	// Check if the volume is ephemeral (If device file exists in the target path's parent directory).
	// 1) Extract the parent directory path from targetPath (Staged device file directory)
	// 2) Check if the device file 'deviceInfo.json' exists in the directory.
	devFileDir := filepath.Dir(targetPath)
	devFilePath := path.Join(devFileDir, deviceInfoFileName)
	exists, _, err := util.FileExists(devFilePath)
	if err != nil {
		return false, fmt.Errorf("Failed to check if device info file %s exists, %v", devFilePath, err.Error())
	}
	if exists {
		// Ephemeral volume
		log.Tracef("Found [%s]. This is ephemeral target path", devFilePath)
		return true, nil
	}

	// Another way to check if it is ephemeral.
	// Check if 'ephemeral_data.json' exists
	ephemeralDataFilePath := path.Join(devFileDir, ephemeralDataFileName)
	exists, _, err = util.FileExists(ephemeralDataFilePath)
	if err != nil {
		return false, fmt.Errorf("Failed to check if ephemeral data file %s exists, %v", devFilePath, err.Error())
	}
	if exists {
		// Ephemeral volume
		log.Tracef("Found [%s]. This is ephemeral target path", ephemeralDataFilePath)
		return true, nil
	}

	// Another way to check if it is ephemeral.
	// Check if 'vol_data.json' contains ["driverMode" : "ephemeral"]
	volData, err := loadVolumeData(devFileDir, volDataFileName)
	if err != nil {
		return false, err
	}
	if volData[driverModeKey] == ephemeralVolumeMode || volData[volumeLifecycleModeKey] == ephemeralVolumeMode {
		// Ephemeral volume
		log.Tracef("Found driverMode or volumeLifecycleMode as 'ephemeral' in [%s]. This is ephemeral target path", volDataFileName)
		return true, nil
	}

	return false, nil
}

func (driver *Driver) nodeUnpublishVolume(targetPath string) error {
	log.Tracef(">>>>> nodeUnpublishVolume, targetPath: %s", targetPath)
	defer log.Trace("<<<<< nodeUnpublishVolume")

	// Block volume: Check for symlink and remove it
	_, symlink, _ := util.IsFileSymlink(targetPath)
	if symlink {
		// Remove the symlink
		log.Tracef("Removing the symlink from target path %s", targetPath)
		if err := util.FileDelete(targetPath); err != nil {
			return status.Error(codes.Internal,
				fmt.Sprintf("Error removing the symlink target path %s, err: %s",
					targetPath, err.Error()))
		}
		return nil
	}

	// Else Mount volume: Unmount the filesystem
	log.Trace("Unmounting filesystem from target path " + targetPath)
	err := driver.chapiDriver.BindUnmount(targetPath)
	if err != nil {
		return status.Error(codes.Internal,
			fmt.Sprintf("Error unmounting target path %s, err: %s", targetPath, err.Error()))
	}
	return nil
}

func (driver *Driver) nodeUnpublishEphemeralVolume(volumeHandle string, targetPath string) error {
	log.Tracef(">>>>> nodeUnpublishEphemeralVolume, volumeHandle: %s, targetPath: %s", volumeHandle, targetPath)
	defer log.Trace("<<<<< nodeUnpublishEphemeralVolume")

	// serialize ephemeral NodeUnpublish() requests
	ephemeralUnpublishLock.Lock()
	defer ephemeralUnpublishLock.Unlock()

	// For ephemeral inline volume, we do the following in-order:
	// 1) Node unpublish (unmount)
	// 2) Node unstage (Remove device and staging device info)
	// 3) Controller unpublish (Remove ACL)
	// 4) Destroy volume

	// 1) Node Unpublish volume (unmount)
	if err := driver.nodeUnpublishVolume(targetPath); err != nil {
		log.Errorf("err: %s", err.Error())
		return err
	}
	log.Infof("Node unpublished ephemeral volume %s from targetPath %s", volumeHandle, targetPath)

	// For ephemeral volume, targetPath's parent directory will be used as 'stagingTargetPath'
	// The device info file is available in this directory.
	stagingTargetPath := filepath.Dir(targetPath)

	// 2) Node Unstage volume (remove device, staging unmount and delete device file)
	if err := driver.nodeUnstageVolume(volumeHandle, stagingTargetPath); err != nil {
		log.Errorf("Error node unstaging the ephemeral volume %s, err: %s", volumeHandle, err.Error())
		return err
	}
	log.Infof("Unstaged the ephemeral volume %s from stagingTargetPath %s",
		volumeHandle, stagingTargetPath)

	// Fetch the volume from the backend using data from ephemeral_data.json file

	// Load Ephemeral data
	ephemeralData, err := loadEphemeralData(stagingTargetPath, ephemeralDataFileName)
	if err != nil {
		log.Errorf("Failed to load ephemeral data from %s, %s", ephemeralDataFileName, err.Error())
		return status.Error(codes.Internal, err.Error())
	}
	if ephemeralData == nil {
		log.Infof("Ephemeral data not found for volume %s, return success", volumeHandle)
		return nil
	}

	// Fetch volume from backend
	// Fetch credentials from secret reference
	log.Tracef("Fetching credentials from ephemeral data %v", ephemeralData)
	if ephemeralData.SecretRef == nil {
		return status.Error(codes.Internal,
			fmt.Sprintf("Missing Secret reference in the ephemeral data %v. Unable to fetch credentials for ephemeral volume %s with ID %s",
				ephemeralData, ephemeralData.VolumeHandle, ephemeralData.VolumeID))
	}
	secrets, err := driver.flavor.GetCredentialsFromSecret(ephemeralData.SecretRef.Name, ephemeralData.SecretRef.Namespace)
	if err != nil {
		log.Error("err: ", err.Error())
		return status.Error(codes.Unavailable,
			fmt.Sprintf("Failed to fetch credentials from secret %s/%s for ephemeral volume %s, err: %s",
				ephemeralData.SecretRef.Name, ephemeralData.SecretRef.Namespace, ephemeralData.VolumeHandle, err.Error()))
	}
	// Get storage provider using secrets
	storageProvider, err := driver.GetStorageProvider(secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return status.Error(codes.Unavailable,
			fmt.Sprintf("Failed to get storage provider from secrets for volume %s with id %s, err: %s",
				ephemeralData.VolumeHandle, ephemeralData.VolumeID, err.Error()))
	}
	// Fetch the volume from the backend using volume ID
	volume, err := storageProvider.GetVolume(ephemeralData.VolumeID)
	if err != nil {
		log.Error("err: ", err.Error())
		return status.Error(codes.Internal,
			fmt.Sprintf("Error while retrieving ephemeral volume %s with id %s from the backend, err: %s",
				ephemeralData.VolumeHandle, ephemeralData.VolumeID, err.Error()))
	}
	if volume == nil {
		// If volume is not found, then return success and skip both ControllerUnpublish() and DeleteVolume()
		log.Infof("Ephemeral volume %s with ID %s not found on the backend, return success",
			ephemeralData.VolumeHandle, ephemeralData.VolumeID)
		// Remove ephemeral data file as volume does not exists anymore
		removeDataFile(stagingTargetPath, ephemeralDataFileName)
		return nil
	}

	// 3) Controller Unpublish Volume
	// Get Node Info
	nodeID, err := driver.nodeGetInfo()
	if err != nil {
		log.Tracef("Failed to get node info, err: %s", err.Error())
		return status.Error(codes.Internal,
			fmt.Sprintf("Failed to node unpublish ephemeral volume %s, err: %s",
				volumeHandle, err.Error()))
	}
	// Controller Unpublish
	if err := driver.controllerUnpublishVolume(volume.ID, nodeID, secrets); err != nil {
		log.Error("err: ", err.Error())
		return err
	}
	log.Infof("Controller unpublished the ephemeral volume %s with ID %s from nodeID %s",
		volumeHandle, volume.ID, nodeID)

	// 4) Delete Volume
	// Destroy the volume from the backend.
	log.Tracef("Destroying ephemeral volume %s with ID %s", volumeHandle, volume.ID)
	err = storageProvider.DeleteVolume(volume.ID, true /* Force Destroy */)
	if err != nil {
		log.Error("err: ", err.Error())
		return status.Error(codes.Internal,
			fmt.Sprintf(" %s with ID %s from the backend, err: %s",
				volumeHandle, volume.ID, err.Error()))
	}
	log.Infof("Destroyed the ephemeral volume %s with ID %s", volumeHandle, volume.ID)

	// Remove ephemeral data file as volume does not exists anymore
	removeDataFile(stagingTargetPath, ephemeralDataFileName)

	// Delete DB entry
	if err := driver.RemoveFromDB(volume.Name); err != nil {
		return err
	}

	log.Infof("Successfully node unpublished the ephemeral volume %s from targetPath %s", volumeHandle, targetPath)
	return nil
}

// NodeGetVolumeStats ...
//
// A Node plugin MUST implement this RPC call if it has GET_VOLUME_STATS node capability. NodeGetVolumeStats RPC call returns the volume
// capacity statistics available for the volume.
//
// If the volume is being used in BlockVolume mode then used and available MAY be omitted from usage field of NodeGetVolumeStatsResponse.
// Similarly, inode information MAY be omitted from NodeGetVolumeStatsResponse when unavailable.
// nolint: dupl
func (driver *Driver) NodeGetVolumeStats(ctx context.Context, in *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	log.Trace(">>>>> NodeGetVolumeStats")
	defer log.Trace("<<<<< NodeGetVolumeStats")

	return nil, status.Error(codes.Unimplemented, "")
}

// NodeExpandVolume ...
//
// A Node Plugin MUST implement this RPC call if it has EXPAND_VOLUME node capability. This RPC call allows CO to expand volume on a node.
//
// NodeExpandVolume ONLY supports expansion of already node-published or node-staged volumes on the given volume_path.
//
// If plugin has STAGE_UNSTAGE_VOLUME node capability then:
//  - NodeExpandVolume MUST be called after successful NodeStageVolume.
//  - NodeExpandVolume MAY be called before or after NodePublishVolume.
// Otherwise NodeExpandVolume MUST be called after successful NodePublishVolume.
// Handles both filesystem type device and raw block device
// TODO assuming expand to underlying device size irrespective of provided capacity range. Need to add support of FS resize to fixed capacity eventhough underlying device is much bigger.
// nolint: dupl
func (driver *Driver) NodeExpandVolume(ctx context.Context, request *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	log.Trace(">>>>> NodeExpandVolume for volume path", request.GetVolumePath())
	defer log.Trace("<<<<< NodeExpandVolume")

	if request.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided for NodeExpandVolume")
	}

	if request.GetVolumePath() == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume path must be provided for NodeExpandVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s", "NodeExpandVolume", request.VolumeId, request.VolumePath)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	accessType := model.MountType

	// VolumeCapability is only available from CSI spec v1.2
	if request.GetVolumeCapability() != nil {
		switch request.GetVolumeCapability().GetAccessType().(type) {
		case *csi.VolumeCapability_Block:
			accessType = model.BlockType
		}
	} else if strings.HasPrefix(request.GetVolumePath(), "/dev/") {
		// for raw block device volume-path is device path: i.e /dev/dm-1
		accessType = model.BlockType
	}

	var err error
	targetPath := ""
	if accessType == model.BlockType {
		// for raw block device volume-path is device path: i.e /dev/dm-1
		targetPath = request.GetVolumePath()
		// Expand device to underlying volume size
		log.Infof("About to expand device %s with access type block to underlying volume size",
			request.VolumePath)
		err = driver.chapiDriver.ExpandDevice(targetPath, model.BlockType)
	} else {
		// figure out if volumePath is actually a staging path
		stagedDevice, err := readStagedDeviceInfo(request.GetVolumePath())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Cannot get staging device info from volume path %s", err.Error()))
		}
		if stagedDevice == nil || stagedDevice.Device == nil {
			return nil, status.Error(codes.Internal,
				fmt.Sprintf("Invalid staging device info found in the path %s. Staging device cannot be nil",
					request.GetVolumePath()))
		}
		if stagedDevice.MountInfo == nil {
			return nil, status.Error(codes.Internal,
				fmt.Sprintf("Missing mount info in the staging device %v. Mount info cannot be nil",
					stagedDevice))
		}
		// Mount point
		targetPath = stagedDevice.MountInfo.MountPoint
		// Expand device to underlying volume size
		log.Infof("About to expand device %s with access type mount to underlying volume size",
			request.VolumePath)
		err = driver.chapiDriver.ExpandDevice(targetPath, model.MountType)
	}

	if err != nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("Unable to expand device, %s", err.Error()))
	}
	// no need to report device capacity here
	return &csi.NodeExpandVolumeResponse{}, nil
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

	// Get max volume per node from environment variable
	nodeMaxVolumesLimit := driver.nodeGetIntEnv(maxVolumesPerNodeKey)

	// Maximum number of volumes that controller can publish to the node.
	// If value is not set or zero CO SHALL decide how many volumes of
	// this type can be published by the controller to the node. The
	// plugin MUST NOT set negative values here.
	// This field is OPTIONAL.
	if nodeMaxVolumesLimit <= 0 {
		nodeMaxVolumesLimit = defaultMaxVolPerNode
	}

	// Enable watcher only once. GetNodeInfo rpc may be called multiple times by external
	// provisioner.
	if !isWatcherEnabled {
		// Create a anonymous wrapper function over nodeGetInfo. This is os event driven
		// fn execution.
		getNodeInfoFunc := func() {
			nodeID, err := driver.nodeGetInfo()
			if err != nil {
				log.Errorf("Failed to update %s nodeInfo. Error: %s", nodeID, err.Error())
			}
			return
		}
		// Register anonymous wrapper function(watcher).
		watcher, _ := util.InitializeWatcher(getNodeInfoFunc)
		// Add list of files /and directories to watch. The list contains
		// iSCSI , FC and CHAP Info and Networking config directories
		list := []string{"/etc/sysconfig/network-scripts/", "/etc/sysconfig/network/", "/etc/iscsi/initiatorname.iscsi", "/etc/networks", "/etc/iscsi/iscsid.conf"}
		watcher.AddWatchList(list)
		// Start event the watcher in a separate thread.
		go watcher.StartWatcher()
		isWatcherEnabled = true
	}
	return &csi.NodeGetInfoResponse{
		NodeId:            nodeID,
		MaxVolumesPerNode: nodeMaxVolumesLimit,
	}, nil
}

// Get cluster node environment variable, convert it to int64
func (driver *Driver) nodeGetIntEnv(envKey string) int64 {
	// Read max_volume_per_node from env
	envVal := os.Getenv(envKey)
	// Check if env variable is set by user
	if envVal == "" {
		return 0
	}
	val, err := strconv.ParseInt(envVal, 10, 64)
	if err != nil {
		log.Warnf("Failed to read cluster node %s setting. Error: %s", envKey, err.Error())
		return 0
	}
	return val
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
	var chapUsername string
	var chapPassword string
	for _, initiator := range initiators {
		if initiator.Type == iscsi {
			for i := 0; i < len(initiator.Init); i++ {
				iqns = append(iqns, &initiator.Init[i])
				// we support only single host initiator
				// check if CHAP credentials is set through configMap, ignore iscsid.conf reference
				envChapUser := os.Getenv(chapUserEnvKey)
				envChapPassword := os.Getenv(chapPasswordEnvKey)
				if envChapUser != "" && envChapPassword != "" {
					chapUsername = envChapUser
					chapPassword = envChapPassword
				}
			}
		} else {
			for i := 0; i < len(initiator.Init); i++ {
				wwpns = append(wwpns, &initiator.Init[i])
			}
		}
	}

	var cidrNetworks []*string
	for _, network := range networks {
		log.Infof("Processing network named %s with IpV4 CIDR %s", network.Name, network.CidrNetwork)
		if network.CidrNetwork != "" {
			cidrNetwork := network.CidrNetwork
			cidrNetworks = append(cidrNetworks, &cidrNetwork)
		}
	}

	node := &model.Node{
		Name:         hostNameAndDomain[0],
		UUID:         host.UUID,
		Iqns:         iqns,
		Networks:     cidrNetworks,
		Wwpns:        wwpns,
		ChapUser:     chapUsername,
		ChapPassword: chapPassword,
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
		log.Errorf("Error reading the device info file %s, err: %s", filePath, err.Error())
		return nil, err
	}

	// Decode into device object
	var stagingDev StagingDevice
	err = json.Unmarshal(deviceInfo, &stagingDev)
	if err != nil {
		log.Error("Error unmarshalling the staged device, err: ", err.Error())
		return nil, err
	}

	return &stagingDev, nil
}

// loadEphemeralData loads ephemeral data from specified json file/location
/* SAMPLE EPHEMERAL DATA FILE CONTENTS: 'ephemeral_data.json'
{
	"secret_ref" : {
		"namespace" : "kube-system",
		"name" : "nimble-secret"
	},
	"pod_data" : {
		"namespace" : "kube-system",
		"uid" : "daa1c178-9666-4afd-85fe-7906e1384c05",
		"name" : "my-pod-inline-mount-test"
	},
	"volume_handle" : "csi-f4f081da0a1ec68b65231cf29fe0d332330eaa136f0284d72c447f49ce665d87",
	"volume_id" : "0601a32bf8f45646a50000000000000000000005fd"
}
*/
func loadEphemeralData(dir string, fileName string) (*Ephemeral, error) {
	dataFilePath := filepath.Join(dir, fileName)
	log.Tracef("loading data file [%s]", dataFilePath)

	// Check if the fileexists
	exists, _, _ := util.FileExists(dataFilePath)
	if !exists {
		log.Infof("Ephemeral data file %s does not exist. Returning here", dataFilePath)
		return nil, nil // Ephemeral volume data not found
	}

	file, err := os.Open(dataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open volume data file [%s], %v", dataFilePath, err)
	}
	defer file.Close()
	data := Ephemeral{}
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to parse data file [%s], %v", dataFilePath, err)
	}
	log.Tracef("Data file [%s] loaded successfully", dataFilePath)
	return &data, nil
}

// loadVolumeData loads volume data from specified json file/location
/* SAMPLE VOLUME DATA FILE CONTENTS: 'vol_data.json'
{
	"volumeHandle" : "csi-f4f081da0a1ec68b65231cf29fe0d332330eaa136f0284d72c447f49ce665d87",
	"driverMode" : "ephemeral",
	"driverName" : "csi.hpe.com",
	"nodeName" : "suneeth-k8s-sjc-2.vlab.nimblestorage.com",
	"specVolID" : "my-volume-1",
	"attachmentID" : "csi-81d5ddd664b04e64b8b5a19a0f266237978584d1478598a8bd970c5e25f88484"
}
*/
func loadVolumeData(dir string, fileName string) (map[string]string, error) {
	dataFilePath := filepath.Join(dir, fileName)
	log.Tracef("loading data file [%s]", dataFilePath)

	// Check if the fileexists
	exists, _, _ := util.FileExists(dataFilePath)
	if !exists {
		log.Infof("Volume data file %s does not exist. Returning here", dataFilePath)
		return nil, nil // Volume data not found
	}

	file, err := os.Open(dataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open volume data file [%s], %v", dataFilePath, err)
	}
	defer file.Close()
	data := map[string]string{}
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to parse data file [%s], %v", dataFilePath, err)
	}
	log.Tracef("Data file [%s] loaded successfully", dataFilePath)
	return data, nil
}
