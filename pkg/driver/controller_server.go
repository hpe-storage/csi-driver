// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/util"
)

// Retrieves volume access type: 'block' or 'filesystem'
func (driver *Driver) getVolumeAccessType(volCap *csi.VolumeCapability) (model.VolumeAccessType, error) {
	log.Tracef(">>>>> getVolumeAccessType, volCap: %+v", volCap)
	defer log.Trace("<<<<< getVolumeAccessType")

	if volCap.GetAccessType() == nil {
		return model.UnknownType, fmt.Errorf("Missing access type in the volume capability %+v", volCap)
	}

	switch valType := volCap.GetAccessType().(type) {
	case *csi.VolumeCapability_Block: // BLOCK ACCESS TYPE
		log.Traceln("Found block access type", volCap.GetBlock())
		return model.BlockType, nil

	case *csi.VolumeCapability_Mount: // MOUNT ACCESS TYPE
		log.Traceln("Found mount access type", volCap.GetMount())
		return model.MountType, nil

	default:
		log.Errorln("Found unsupported access type", valType)
		return model.UnknownType, fmt.Errorf("Found unsupported access type %v", valType)
	}
}

// ValidateAndGetVolumeAccessType from the list of requested volume capabilities.
func (driver *Driver) ValidateAndGetVolumeAccessType(volCaps []*csi.VolumeCapability) (model.VolumeAccessType, error) {
	log.Tracef(">>>>> ValidateAndGetVolumeAccessType, volCaps: %+v", volCaps)
	defer log.Trace("<<<<< ValidateAndGetVolumeAccessType")

	/* Note: The volume capabilities must specify only one of
	two access types (Block or Mount), but not both.
	If more than one access type is specified, return appropriate error.
	*/
	var volAccessType model.VolumeAccessType
	isBlockType := false
	isMountType := false
	for _, cap := range volCaps {
		var err error
		volAccessType, err = driver.getVolumeAccessType(cap)
		if err != nil {
			return model.UnknownType, err
		}
		if volAccessType == model.BlockType {
			// Found Block Access
			isBlockType = true
		}
		if volAccessType == model.MountType {
			// Found Mount Access
			isMountType = true
		}
		// Both Block type and Mount type cannot be requested for the same volume
		if isBlockType == true && isMountType == true {
			return model.UnknownType, fmt.Errorf("Found more than one access type present in the volume capabilities")
		}
	}
	log.Tracef("Retrieved volume access type: %v", volAccessType.String())
	return volAccessType, nil
}

// IsValidVolumeCapability checks if the given volume capability is supported by the driver
// nolint : gcyclo
func (driver *Driver) IsValidVolumeCapability(volCap *csi.VolumeCapability) (bool, error) {
	log.Tracef(">>>>> IsValidVolumeCapability, volCapability: %+v", volCap)
	defer log.Trace("<<<<< IsValidVolumeCapability")

	// Access-Mode: This is a mandatory field
	if volCap.GetAccessMode() == nil {
		log.Error("Access mode is missing in the specified volume capability")
		return false, status.Error(
			codes.InvalidArgument, "Volume capability must contain access mode in the request")
	}
	log.Traceln("Found access_mode:", volCap.GetAccessMode().GetMode())

	// Try to match with the list of supported accessModes by the driver
	found := false
	for _, volCapAccessMode := range driver.volumeCapabilityAccessModes {
		if volCapAccessMode.GetMode() == volCap.GetAccessMode().GetMode() {
			found = true
			break
		}
	}
	if !found {
		return false, status.Error(
			codes.InvalidArgument,
			fmt.Sprintf("Volume capability with access-mode %v not supported by the driver", volCap.GetAccessMode().GetMode()))
	}

	// Access-Type: This is an optional field
	if volCap.GetAccessType() != nil {
		// Validate the access_type: Block or Mount
		switch valType := volCap.GetAccessType().(type) {
		case *csi.VolumeCapability_Block: // BLOCK ACCESS TYPE
			log.Traceln("Found Block access_type", volCap.GetBlock())
			break
		case *csi.VolumeCapability_Mount: // MOUNT ACCESS TYPE
			log.Traceln("Found Mount access_type", volCap.GetMount())
			if volCap.GetMount() != nil {
				fileSystem := volCap.GetMount().GetFsType()
				log.Traceln("Found Mount access_type, FileSystem:", fileSystem)

				// Currently, we don't support NFS mount
				if strings.ToLower(fileSystem) == nfsFileSystem {
					return false, status.Error(codes.InvalidArgument, "NFS mount is not supported by the driver")
				}

				// Note: `mount_flags` MAY contain sensitive information.
				// Therefore, the CO and the Driver MUST NOT leak this information to untrusted entities.
				// Check if the total size of mount_flags specified does not exceed 4KiB (max allowed limit)
				totalLen := int64(0)
				for _, mflag := range volCap.GetMount().GetMountFlags() {
					totalLen += int64(len(mflag))
				}
				log.Traceln("Total length of mount flags:", totalLen)
				if totalLen > mountFlagsSizeMaxAllowed {
					log.Errorln("Mount flags size exceeded 4KiB limit")
					return false, status.Error(codes.InvalidArgument,
						fmt.Sprintf("Volume capability has mount flags of size %d, exceeding the maximum allowed limit of 4KiB", totalLen))
				}
			}
			break
		default:
			log.Errorln("Received unsupported access type", valType)
			return false, status.Error(
				codes.InvalidArgument, fmt.Sprintf("Found unsupported access type %v", valType))
		}
	}
	return true, nil
}

// AreVolumeCapabilitiesSupported verifies if the given volcaps are supported by the driver
// nolint : gocyclo
func (driver *Driver) AreVolumeCapabilitiesSupported(volCapabilities []*csi.VolumeCapability) (bool, error) {
	log.Tracef(">>>>> AreVolumeCapabilitiesSupported, volCapabilities : %+v", volCapabilities)
	defer log.Trace("<<<<< AreVolumeCapabilitiesSupported")

	for _, volCap := range volCapabilities {
		log.Tracef("Validating volCapability: %+v", volCap)
		_, err := driver.IsValidVolumeCapability(volCap)
		if err != nil {
			log.Errorf("Found unsupported volume capability %+v, err: %v", volCap, err.Error())
			return false, err
		}
	}
	return true, nil
}

// CreateVolume ...
//
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_VOLUME controller capability. This RPC will be called by the CO to
// provision a new volume on behalf of a user (to be consumed as either a block device or a mounted filesystem).
//
// This operation MUST be idempotent. If a volume corresponding to the specified volume name already exists, is accessible from
// accessibility_requirements, and is compatible with the specified capacity_range, volume_capabilities and parameters in the CreateVolumeRequest,
// the Plugin MUST reply 0 OK with the corresponding CreateVolumeResponse.
//
// Plugins MAY create 3 types of volumes:
//
// Empty volumes. When plugin supports CREATE_DELETE_VOLUME OPTIONAL capability.
// From an existing snapshot. When plugin supports CREATE_DELETE_VOLUME and CREATE_DELETE_SNAPSHOT OPTIONAL capabilities.
// From an existing volume. When plugin supports cloning, and reports the OPTIONAL capabilities CREATE_DELETE_VOLUME and CLONE_VOLUME.
// nolint : gocyclo
func (driver *Driver) CreateVolume(ctx context.Context, request *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Trace(">>>>> CreateVolume")
	defer log.Trace("<<<<< CreateVolume")

	log.Infof("CreateVolume requested volume '%s' with the following Capabilities: '%v' and Parameters: '%v'",
		request.Name, request.VolumeCapabilities, request.Parameters)

	// Name
	if request.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name must be provided to create a volume")
	}

	// VolumeCapabilities
	if request.VolumeCapabilities == nil || len(request.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities are required to create a volume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s", "CreateVolume", request.Name)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Create DB entry
	dbKey := request.Name
	if err := driver.AddToDB(dbKey, Pending); err != nil {
		return nil, err
	}
	defer driver.RemoveFromDBIfPending(dbKey)

	// CapacityRange
	reqVolumeSize := int64(defaultVolumeSize) // Default value
	if request.GetCapacityRange() != nil {
		reqVolumeSize = request.GetCapacityRange().GetRequiredBytes()
		log.Info("Requested for capacity bytes: ", reqVolumeSize)
		// Note: 'limit_bytes' not supported ???
	}

	createParameters, err := driver.flavor.ConfigureAnnotations(request.Name, request.Parameters)
	if err != nil {
		log.Errorf("Failed to configure create parameters from PVC annotations. err=%v", err)
		return nil, status.Error(codes.Internal, "Failed to configure create parameters from PVC annotations")
	}

	// Create volume
	volume, err := driver.createVolume(
		request.Name,
		reqVolumeSize,
		request.VolumeCapabilities,
		request.Secrets,
		request.VolumeContentSource,
		createParameters,
	)
	if err != nil {
		log.Errorf("Volume creation failed, err: %s", err.Error())
		return nil, err
	}

	// Update DB entry
	if err := driver.UpdateDB(dbKey, volume); err != nil {
		return nil, err
	}

	return &csi.CreateVolumeResponse{
		Volume: volume,
	}, nil
}

// nolint: gocyclo
func (driver *Driver) createVolume(
	name string,
	size int64,
	volumeCapabilities []*csi.VolumeCapability,
	secrets map[string]string,
	volumeContentSource *csi.VolumeContentSource,
	createParameters map[string]string) (*csi.Volume, error) {

	log.Tracef(">>>>> createVolume, name: %s, size: %d, volumeCapabilities: %v, createParameters: %v",
		name, size, volumeCapabilities, createParameters)
	defer log.Trace("<<<<< createVolume")

	// Check if the requested volume capabilities are supported.
	_, err := driver.AreVolumeCapabilitiesSupported(volumeCapabilities)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Found one or more unsupported volume capabilities: %+v", volumeCapabilities))
	}

	// Validate and Get volume access type
	volAccessType, err := driver.ValidateAndGetVolumeAccessType(volumeCapabilities)
	if err != nil {
		log.Errorf("Failed to validate and retrieve volume access type, err: %v", err.Error())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to validate and retrieve volume access type, %v", err.Error()))
	}

	// The volume access type in the request will be either 'Block' or 'Mount'
	// If none, then we default to 'Mount' access type and use default filesystem.
	var filesystem string
	if volAccessType == model.MountType {
		foundFsType := false
		// Only one FS type must be specified in the volcaps list.
		// If more than one is requested, then return appropriate error.
		for _, cap := range volumeCapabilities {
			// Get filesystem type
			if cap.GetMount() != nil && cap.GetMount().GetFsType() != "" {
				if foundFsType && filesystem != cap.GetMount().GetFsType() {
					log.Errorf("Found more than one FS types: [%s, %s]", filesystem, cap.GetMount().GetFsType())
					return nil, status.Error(codes.InvalidArgument, "Request has more than one filesystem type in the volume capabilities, but only one is expected")
				}
				filesystem = cap.GetMount().GetFsType()
				foundFsType = true
			}
		}
		// Using default FS
		if filesystem == "" {
			filesystem = defaultFileSystem
			log.Trace("Using default filesystem type: ", filesystem)
		}
		if driver.IsSupportedMultiNodeAccessMode(volumeCapabilities) && !driver.IsNFSResourceRequest(createParameters) {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("StorageClass parameter %s=%s is missing for creation of volumes with multi-node access", nfsResourcesKey, trueKey))
		}
	}

	// Verify if NFS based provisioning is requested
	if driver.IsNFSResourceRequest(createParameters) {
		// check if block access type is requested with multi-node mode
		if volAccessType == model.BlockType {
			return nil, status.Error(codes.InvalidArgument, "NFS volume provisioning is not supported with block access type")
		}

		volume, rollback, err := driver.flavor.CreateNFSVolume(name, size, createParameters, volumeContentSource)
		if err == nil {
			// Return multi-node volume
			return volume, nil
		}

		rollbackStatus := "success"
		if rollback {
			// get nfs namespace for cleanup
			nfsNamespace := defaultNFSNamespace
			if namespace, ok := createParameters[nfsNamespaceKey]; ok {
				nfsNamespace = namespace
			}

			nfsResourceName := fmt.Sprintf("%s-%s", "hpe-nfs", strings.TrimPrefix(name, "pvc-"))
			// attempt to teardown all nfs resources
			err2 := driver.flavor.RollbackNFSResources(nfsResourceName, nfsNamespace)
			if err2 != nil {
				log.Errorf("failed to rollback NFS resources for %s, err %s", name, err2.Error())
				rollbackStatus = err2.Error()
			}
		}
		errStr := fmt.Sprintf("Failed to create NFS provisioned volume %s, err %s, rollback status: %s", name, err.Error(), rollbackStatus)
		log.Errorf(errStr)
		return nil, status.Error(codes.Internal, errStr)
	}

	// TODO: use additional properties here to configure the volume further... these might come in from doryd
	createOptions := make(map[string]interface{})
	for k, v := range createParameters {
		// convert create options to snakecase before passing it to CSP
		createOptions[util.ToSnakeCase(k)] = v
	}

	// Get the multi-initiator access mode
	multiInitiator := driver.IsSupportedMultiNodeAccessMode(volumeCapabilities)
	createOptions[util.ToSnakeCase(multiInitiatorKey)] = multiInitiator

	log.Trace("Volume create options: ", createOptions)

	// extract the description if present
	var description string
	if val, ok := createOptions[descriptionKey]; ok {
		description = fmt.Sprintf("%v", val)
		delete(createOptions, descriptionKey)
	}

	// Build the volume context to be returned to the CO in the create response
	// This same context will be used by the CO during Publish and Stage workflows
	respVolContext := make(map[string]string)
	if createParameters != nil {
		// Copy the request parameters into resp context
		respVolContext = createParameters
	}
	// Block or Mount type
	respVolContext[volumeAccessModeKey] = volAccessType.String()
	// For block access, the filesystem will be empty.
	if filesystem != "" {
		log.Trace("Adding filesystem to the volume context, Filesystem: ", filesystem)
		respVolContext[fsTypeKey] = filesystem
	}
	log.Trace("Volume context in response to CO: ", respVolContext)

	// Secrets
	storageProvider, err := driver.GetStorageProvider(secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to get storage provider from secrets, %s", err.Error()))
	}

	// TODO: check for version compatibility for new features

	// Note the call to GetVolumeByName.  Names are only unique within a single group so a given name
	// can be used more than once if multiple groups are configured.  Note the comment from the spec regarding
	// the name field:
	// It serves two purposes:
	// 1) Idempotency - This name is generated by the CO to achieve
	//    idempotency.  The Plugin SHOULD ensure that multiple
	//    `CreateVolume` calls for the same name do not result in more
	//    than one piece of storage provisioned corresponding to that
	//    name. If a Plugin is unable to enforce idempotency, the CO's
	//    error recovery logic could result in multiple (unused) volumes
	//    being provisioned.
	//
	// I don't think the spec considers the possiblity of reusing names across multiple groups.
	// We should ask a question on the email group.
	existingVolume, err := storageProvider.GetVolumeByName(name)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable, "Failed to check if volume exists")
	}

	if existingVolume != nil {
		log.Trace("Volume found :", existingVolume.Name)
		log.Trace("existingVolume.id :", existingVolume.ID)
		log.Trace("existingVolume.Size :", existingVolume.Size)

		// Check if the existing volume's size matches with the requested size
		// TODO: Nimble doesn't support capacity range, but other SP might.
		// We may consider adding range support in the future???
		if existingVolume.Size != size {
			log.Errorf("Volume already exists with size %v but different size %v being requested.",
				existingVolume.Size, size)
			return nil, status.Error(
				codes.AlreadyExists,
				fmt.Sprintf("Volume %s with size %v already exists but different size %v being requested.",
					name, existingVolume.Size, size))
		}
		// update volume context with volume parameters
		updateVolumeContext(respVolContext, existingVolume)
		// Return existing volume with volume context info
		log.Tracef("Returning the existing volume '%s' with size %d", existingVolume.Name, existingVolume.Size)
		return &csi.Volume{
			VolumeId:      existingVolume.ID,
			CapacityBytes: existingVolume.Size,
			VolumeContext: respVolContext,
		}, nil
	}

	// Create volume using pre-populated content if requested
	if volumeContentSource != nil {
		log.Tracef("Request with volumeContentSource: %+v", volumeContentSource) // Verify source snap if specified
		if volumeContentSource.GetSnapshot() != nil {
			// Check if the driver supports SNAPSHOT service
			if !driver.IsSupportedControllerCapability(csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT) {
				return nil, status.Error(codes.InvalidArgument, "Unable to create volume from snapshot. Driver does not support snapshot create/delete")
			}

			snapID := volumeContentSource.GetSnapshot().SnapshotId
			// Check if the specified snapshot exists on the CSP
			log.Trace("Lookup snapshot with ID ", snapID)
			existingSnap, err := storageProvider.GetSnapshot(snapID)
			if err != nil {
				log.Error("Failed to process volume source, err: ", err.Error())
				return nil, status.Error(codes.Internal, "Error while looking for base snapshot with ID "+snapID)
			}
			if existingSnap == nil {
				return nil, status.Error(codes.NotFound, "Could not find base snapshot with ID "+snapID)
			}
			log.Tracef("Found snapshot: %#v", existingSnap)

			// Check if parent volume ID is present
			if existingSnap.VolumeID == "" {
				return nil, status.Error(codes.Internal, "Parent volume ID is missing in the snapshot response")
			}

			// Get the parent volume from the snapshot
			existingParentVolume, err := storageProvider.GetVolume(existingSnap.VolumeID)
			if err != nil {
				log.Error("err: ", err.Error())
				return nil,
					status.Error(codes.Internal,
						fmt.Sprintf("Failed to check if snapshot's parent volume %s with ID %s exist, err: %s",
							existingSnap.VolumeName, existingSnap.VolumeID, err.Error()))
			}

			if existingParentVolume == nil {
				return nil,
					status.Error(codes.NotFound,
						fmt.Sprintf("Could not find Snapshot's parent volume %s with ID %s",
							existingSnap.VolumeName, existingSnap.VolumeID))

			}
			// Get existing parent volume fsType attribute
			parentVolFsType, err := driver.flavor.GetVolumePropertyOfPV("fsType", existingParentVolume.Name)
			if err != nil {
				log.Error("err: ", err.Error())
				return nil,
					status.Error(codes.Internal,
						fmt.Sprintf("Failed to check if filesystem exists on the %s parent volume, err: %s",
							existingSnap.VolumeName, err.Error()))
			}

			// Check if requested filesystem for a clone volume is same as existing snapshot
			if parentVolFsType != "" && filesystem != parentVolFsType {
				return nil,
					status.Error(codes.InvalidArgument,
						fmt.Sprintf("Requested volume filesystem %s cannot be different than snapshot's parent volume filesystem %s", filesystem, parentVolFsType))
			}
			// Create a clone from another volume
			log.Infof("About to create a new clone '%s' from snapshot %s with options %+v", name, existingSnap.ID, createOptions)
			volume, err := storageProvider.CloneVolume(name, description, "", existingSnap.ID, size, createOptions)
			if err != nil {
				log.Tracef("Clone creation failed, err: %s", err.Error())
				return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to clone-create volume %s, %s", name, err.Error()))
			}
			// update volume context with cloned volume parameters
			updateVolumeContext(respVolContext, volume)
			// Return newly cloned volume (clone from snapshot)
			return &csi.Volume{
				VolumeId:      volume.ID,
				CapacityBytes: volume.Size,
				VolumeContext: respVolContext,
				ContentSource: volumeContentSource,
			}, nil
		}

		// Verify source volume if specified
		if volumeContentSource.GetVolume() != nil {
			volID := volumeContentSource.GetVolume().VolumeId
			// Check if the specified volume exists on the CSP
			log.Trace("Lookup volume with ID ", volID)
			existingParentVolume, err := storageProvider.GetVolume(volID)
			if err != nil {
				log.Errorf("Failed to get volume source, err: %s", err.Error())
				return nil, status.Error(codes.Internal, fmt.Sprintf("Error while looking for parent volume %s, err: %s", volID, err.Error()))
			}
			if existingParentVolume == nil {
				return nil, status.Error(codes.NotFound, "Could not find parent volume with ID "+volID)
			}
			log.Tracef("Found parent volume: %+v", existingParentVolume)

			// The requested size is must be at least equal to the snapshot's parent volume size
			if size != existingParentVolume.Size {
				return nil,
					status.Error(codes.InvalidArgument,
						fmt.Sprintf("Requested clone size %d is not equal to the parent volume size %d", size, existingParentVolume.Size))
			}

			// Create a clone from another volume
			log.Infof("About to create a new clone '%s' of size %v from volume %s with options %+v", name, size, existingParentVolume.ID, createOptions)
			volume, err := storageProvider.CloneVolume(name, description, existingParentVolume.ID, "", size, createOptions)
			if err != nil {
				log.Tracef("Clone creation failed, err: %s", err.Error())
				return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to clone-create volume %s, %s", name, err.Error()))
			}
			// update volume context with cloned volume parameters
			updateVolumeContext(respVolContext, volume)
			// Return newly cloned volume (clone from volume)
			return &csi.Volume{
				VolumeId:      volume.ID,
				CapacityBytes: volume.Size,
				VolumeContext: respVolContext,
				ContentSource: volumeContentSource,
			}, nil
		}
		return nil, status.Error(codes.InvalidArgument, "One of snapshot or parent volume source must be specified")
	}

	// Create new volume
	log.Infof("About to create a new volume '%s' with size %d and options %+v", name, size, createOptions)
	volume, err := storageProvider.CreateVolume(name, description, size, createOptions)
	if err != nil {
		log.Trace("Volume creation failed, err: " + err.Error())
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to create volume %s, %s", name, err.Error()))
	}
	// update volume context with volume parameters
	updateVolumeContext(respVolContext, volume)
	// Return newly created volume with volume context info
	return &csi.Volume{
		VolumeId:      volume.ID,
		CapacityBytes: volume.Size,
		VolumeContext: respVolContext,
	}, nil
}

// DeleteVolume ...
//
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_VOLUME capability. This RPC will be called by the CO to
// deprovision a volume.
//
// This operation MUST be idempotent. If a volume corresponding to the specified volume_id does not exist or the
// artifacts associated with the volume do not exist anymore, the Plugin MUST reply 0 OK.
func (driver *Driver) DeleteVolume(ctx context.Context, request *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Trace(">>>>> DeleteVolume")
	defer log.Trace("<<<<< DeleteVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Invalid Volume ID %s specified for DeleteVolume", request.VolumeId))
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s", "DeleteVolume", request.VolumeId)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Delete by ID
	if err := driver.deleteVolume(request.VolumeId, request.Secrets, false); err != nil {
		log.Errorf("Error deleting the volume %s, err: %s", request.VolumeId, err.Error())
		return nil, err
	}
	return &csi.DeleteVolumeResponse{}, nil
}

func (driver *Driver) deleteVolume(volumeID string, secrets map[string]string, force bool) error {
	log.Tracef(">>>>> deleteVolume, volumeID: %s, force: %v", volumeID, force)
	defer log.Trace("<<<<< deleteVolume")

	// Check if this is a multi-node volume
	if driver.flavor.IsNFSVolume(volumeID) {
		// volumeId represents nfs claim uid for multinode volume
		err := driver.flavor.DeleteNFSVolume(volumeID)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		return nil
	}

	// Get Volume using secrets
	existingVolume, err := driver.GetVolumeByID(volumeID, secrets)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			log.Trace("Could not find volume with ID " + volumeID)
			return nil
		}
		// Error while retrieving the volume using id and secrets
		return err
	}
	log.Tracef("Found Volume %s with ID %s", existingVolume.Name, existingVolume.ID)

	// Check if volume still in published state or ACL exists
	if existingVolume.Published {
		log.Errorf("Volume %s with ID %s still in use", existingVolume.Name, existingVolume.ID)
		// TODO: Return correct error code 'FailedPrecondition' as per CSI spec.
		//       Here, we return 'internal' error code so that the external provisioner performs enough retries to cleanup the volume.
		return status.Errorf(codes.Internal, fmt.Sprintf("Volume %s with ID %s is still in use", existingVolume.Name, existingVolume.ID))
	}

	// Get Storage Provider
	storageProvider, err := driver.GetStorageProvider(secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return status.Error(codes.Unavailable,
			fmt.Sprintf("Failed to get storage provider from secrets, err: %s", err.Error()))
	}

	// Delete the volume from the array
	log.Infof("About to delete volume %s with force=%v", volumeID, force)
	if err := storageProvider.DeleteVolume(volumeID, force); err != nil {
		log.Error("err: ", err.Error())
		return status.Error(codes.Internal,
			fmt.Sprintf("Error while deleting volume %s, err: %s", existingVolume.Name, err.Error()))
	}

	// Delete DB entry
	if err := driver.RemoveFromDB(existingVolume.Name); err != nil {
		return err
	}
	return nil
}

// ControllerPublishVolume ...
//
// A Controller Plugin MUST implement this RPC call if it has PUBLISH_UNPUBLISH_VOLUME controller capability. This RPC will be called
// by the CO when it wants to place a workload that uses the volume onto a node. The Plugin SHOULD perform the work that is necessary
// for making the volume available on the given node. The Plugin MUST NOT assume that this RPC will be executed on the node where the
// volume will be used.
//
// This operation MUST be idempotent. If the volume corresponding to the volume_id has already been published at the node corresponding
// to the node_id, and is compatible with the specified volume_capability and readonly flag, the Plugin MUST reply 0 OK.
//
// If the operation failed or the CO does not know if the operation has failed or not, it MAY choose to call ControllerPublishVolume again
// or choose to call ControllerUnpublishVolume.
//
// The CO MAY call this RPC for publishing a volume to multiple nodes if the volume has MULTI_NODE capability (i.e., MULTI_NODE_READER_ONLY,
// MULTI_NODE_SINGLE_WRITER or MULTI_NODE_MULTI_WRITER).
// nolint: gocyclo
func (driver *Driver) ControllerPublishVolume(ctx context.Context, request *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	log.Trace(">>>>> ControllerPublishVolume")
	defer log.Trace("<<<<< ControllerPublishVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided to ControllerPublishVolume")
	}

	if request.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Node ID not provided to ControllerPublishVolume")
	}

	if request.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "Invalid volume capability specified for ControllerPublishVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s", "ControllerPublishVolume", request.VolumeId, request.NodeId)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Publish volume by adding ACL
	publishContext, err := driver.controllerPublishVolume(
		request.VolumeId,
		request.NodeId,
		request.Secrets,
		request.VolumeCapability,
		request.Readonly,
		request.VolumeContext,
	)
	if err != nil {
		log.Errorf("Error controller publishing volume %s, err: %s", request.VolumeId, err.Error())
		return nil, err
	}

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: publishContext,
	}, nil
}

func (driver *Driver) controllerPublishVolume(
	volumeID string,
	nodeID string,
	secrets map[string]string,
	volumeCapability *csi.VolumeCapability,
	readOnlyFlag bool,
	volumeContext map[string]string) (map[string]string, error) {

	log.Tracef(">>>>> controllerPublishVolume with volumeID: %v, nodeID: %s, volumeCapability: %v, readOnlyFlag: %v, volumeContext: %v",
		volumeID, nodeID, volumeCapability, readOnlyFlag, volumeContext)
	defer log.Trace("<<<<< controllerPublishVolume")

	// Read-Only
	// Indicates SP MUST publish the volume in readonly mode.
	// CO MUST set this field to false if SP does not have the PUBLISH_READONLY controller capability.
	// This is a REQUIRED field
	if !driver.IsSupportedControllerCapability(csi.ControllerServiceCapability_RPC_PUBLISH_READONLY) {
		if readOnlyFlag != false {
			return nil, status.Error(codes.InvalidArgument, "Invalid readonly parameter. It must be set to false")
		}
	}
	if readOnlyFlag {
		// Publish the volume in read-only mode if set to true
		log.Infof("Volume %s will be published with read-only access", volumeID)
	}

	// Check if driver supports the requested volume capability
	_, err := driver.IsValidVolumeCapability(volumeCapability)
	if err != nil {
		log.Errorf("Unsupported volume capability %+v, err: %v", volumeCapability, err.Error())
		return nil, err
	}

	// Get volume access type
	volAccessType, err := driver.getVolumeAccessType(volumeCapability)
	if err != nil {
		log.Errorf("Error retrieving volume access type, err: %v", err.Error())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to retrieve volume access type, %v", err.Error()))
	}

	// Check if the requested access type matches with the volume created access type
	if volumeContext[volumeAccessModeKey] != "" && volumeContext[volumeAccessModeKey] != volAccessType.String() {
		log.Errorf("Volume access type '%v' specified at the creation time mismatched with the requested access type '%v'",
			volumeContext[volumeAccessModeKey], volAccessType.String())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Volume %s created with access type %v, but controller publish requested with access type %v",
				volumeID, volumeContext[volumeAccessModeKey], volAccessType.String()))
	}

	// Check if volume is created with type RO or ROX modes and force ro mode with publish
	// NOTE: required until this is fixed to set request.Readonly correctly: https://github.com/kubernetes/kubernetes/issues/70505
	readOnlyAccessMode := driver.IsReadOnlyAccessMode([]*csi.VolumeCapability{volumeCapability})

	// Check if volume is requested with NFS resources and intercept here
	if driver.IsNFSResourceRequest(volumeContext) {
		// TODO: check and add client ACL here
		log.Info("ControllerPublish requested with NFS resources, returning success")
		return map[string]string{
			volumeAccessModeKey: volAccessType.String(),
			readOnlyKey:         strconv.FormatBool(readOnlyAccessMode),
			nfsMountOptionsKey:  volumeContext[nfsMountOptionsKey],
		}, nil
	}

	// Get Volume using secrets
	volume, err := driver.GetVolumeByID(volumeID, secrets)
	if err != nil {
		log.Errorf("Failed to get volume %s, err: %s", volumeID, err.Error())
		return nil, err
	}

	volumeConfig := make(map[string]interface{})
	for k, v := range volume.Config {
		// convert volume config from CSP to camelCase before parsing it
		volumeConfig[util.ToCamelCase(k)] = v
	}
	log.Tracef("volume config is %+v", volumeConfig)

	// Decode and check if the node is configured
	node, err := driver.flavor.GetNodeInfo(nodeID)
	if err != nil {
		log.Error("Cannot unmarshal node from node ID. err: ", err.Error())
		return nil, status.Error(codes.NotFound, err.Error())
	}

	if node.ChapUser != "" && node.ChapPassword != "" {
		decodedChapPassword, _ := b64.StdEncoding.DecodeString(node.ChapPassword)
		node.ChapPassword = string(decodedChapPassword)
	}

	// Get storageProvider using secrets
	storageProvider, err := driver.GetStorageProvider(secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable,
			fmt.Sprintf("Failed to get storage provider from secrets, err: %s", err.Error()))
	}

	// check if the node context is already available in the CSP
	existingNode, err := storageProvider.GetNodeContext(node.UUID)
	if err != nil {
		log.Errorf("Error retrieving the node info from the CSP. err: %s", err.Error())
		return nil, status.Error(codes.Unavailable,
			fmt.Sprintf("Error retrieving the node info for ID %s from the CSP, err: %s", node.UUID, err.Error()))
	}
	if existingNode != nil {
		log.Tracef("CSP has already been notified about the node with ID %s and UUID %s", existingNode.ID, existingNode.UUID)
	} else {
		// If node does not already exists, then set context here
		log.Tracef("Notifying CSP about Node with ID %s and UUID %s", node.ID, node.UUID)
		if err = storageProvider.SetNodeContext(node); err != nil {
			log.Error("err: ", err.Error())
			return nil, status.Error(codes.Unavailable,
				fmt.Sprintf("Failed to provide node context %v to CSP err: %s", node, err.Error()))
		}
	}

	// Configure access protocol defaulting to iSCSI when unspecified
	var requestedAccessProtocol = volumeContext[accessProtocolKey]
	if requestedAccessProtocol == "" {
		if len(node.Iqns) != 0 {
			requestedAccessProtocol = iscsi
		} else {
			requestedAccessProtocol = fc
		}
		log.Tracef("Defaulting to access protocol %s", requestedAccessProtocol)
	}

	// Add ACL to the volume based on the requested Node ID
	publishInfo, err := storageProvider.PublishVolume(volume.ID, node.UUID, requestedAccessProtocol)
	if err != nil {
		log.Errorf("Failed to publish volume %s, err: %s", volume.ID, err.Error())
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("Failed to add ACL to volume %s for node %v via CSP, err: %s", volume.ID, node, err.Error()))
	}
	log.Tracef("PublishInfo response from CSP: %+v", publishInfo)

	// target scope is nimble specific therefore extract it from the volume config
	var requestedTargetScope = targetScopeGroup
	if val, ok := volumeConfig[targetScopeKey]; ok {
		requestedTargetScope = fmt.Sprintf("%v", val)
	}

	// TODO: add any additional info necessary to mount the device
	publishContext := map[string]string{}
	publishContext[serialNumberKey] = publishInfo.SerialNumber
	publishContext[accessProtocolKey] = publishInfo.AccessInfo.BlockDeviceAccessInfo.AccessProtocol
	publishContext[targetNamesKey] = strings.Join(publishInfo.AccessInfo.BlockDeviceAccessInfo.TargetNames, ",")
	publishContext[targetScopeKey] = requestedTargetScope
	publishContext[lunIDKey] = strconv.Itoa(int(publishInfo.AccessInfo.BlockDeviceAccessInfo.LunID))

	// Start of population of target array details
	if publishInfo.AccessInfo.BlockDeviceAccessInfo.SecondaryBackendDetails.PeerArrayDetails != nil {
		secondaryArrayMarshalledStr, err := json.Marshal(&publishInfo.AccessInfo.BlockDeviceAccessInfo.SecondaryBackendDetails)
		if err != nil {
			log.Errorf("Error in marshalling secondary details %s", err.Error())
			return nil, status.Error(codes.Internal,
				fmt.Sprintf("error in marshalling secondary array details %s", err.Error()))
		}
		log.Tracef("\n Marshalled secondary array str :%v", secondaryArrayMarshalledStr)
		publishContext[secondaryArrayDetailsKey] = string(secondaryArrayMarshalledStr)

	}

	if strings.EqualFold(publishInfo.AccessInfo.BlockDeviceAccessInfo.AccessProtocol, iscsi) {
		publishContext[discoveryIPsKey] = strings.Join(publishInfo.AccessInfo.BlockDeviceAccessInfo.IscsiAccessInfo.DiscoveryIPs, ",")
		// validate chapuser from storage provider and node
		if node.ChapUser != "" && !strings.EqualFold(publishInfo.AccessInfo.BlockDeviceAccessInfo.IscsiAccessInfo.ChapUser, node.ChapUser) {
			err := fmt.Errorf("Failed to publish volume. chapuser expected :%s got :%s", node.ChapUser, publishInfo.AccessInfo.BlockDeviceAccessInfo.IscsiAccessInfo.ChapUser)
			log.Errorf(err.Error())
			return nil, status.Error(codes.Internal, err.Error())
		}
		publishContext[chapUsernameKey] = node.ChapUser
		publishContext[chapPasswordKey] = node.ChapPassword
	}

	if readOnlyAccessMode == true {
		publishContext[readOnlyKey] = strconv.FormatBool(readOnlyAccessMode)
	} else { // Default case, we stick to old behavior
		publishContext[readOnlyKey] = strconv.FormatBool(readOnlyFlag)
	}
	publishContext[volumeAccessModeKey] = volumeContext[volumeAccessModeKey] // Block or Mount type

	// Publish FS details only if 'mount' access is being requested
	if publishContext[volumeAccessModeKey] == model.MountType.String() {
		// Filesystem Details
		log.Trace("Adding filesystem details to the publish context")
		publishContext[fsTypeKey] = volumeContext[fsTypeKey]
		publishContext[fsOwnerKey] = volumeContext[fsOwnerKey]
		publishContext[fsModeKey] = volumeContext[fsModeKey]
		publishContext[fsCreateOptionsKey] = volumeContext[fsCreateOptionsKey]
	}
	log.Tracef("Volume %s with ID %s published with the following details: %+v",
		volume.Name, volume.ID, publishContext)
	return publishContext, nil
}

// ControllerUnpublishVolume ...
//
// Controller Plugin MUST implement this RPC call if it has PUBLISH_UNPUBLISH_VOLUME controller capability. This RPC is a reverse operation of
// ControllerPublishVolume. It MUST be called after all NodeUnstageVolume and NodeUnpublishVolume on the volume are called and succeed. The
// Plugin SHOULD perform the work that is necessary for making the volume ready to be consumed by a different node. The Plugin MUST NOT assume
// that this RPC will be executed on the node where the volume was previously used.
//
// This RPC is typically called by the CO when the workload using the volume is being moved to a different node, or all the workload using the
// volume on a node has finished.
//
// This operation MUST be idempotent. If the volume corresponding to the volume_id is not attached to the node corresponding to the node_id,
// the Plugin MUST reply 0 OK. If this operation failed, or the CO does not know if the operation failed or not, it can choose to call
// ControllerUnpublishVolume again.
func (driver *Driver) ControllerUnpublishVolume(ctx context.Context, request *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	log.Trace(">>>>> ControllerUnpublishVolume")
	defer log.Trace("<<<<< ControllerUnpublishVolume")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided to ControllerUnpublishVolume")
	}

	if request.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Node ID not provided to ControllerUnpublishVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s", "ControllerUnpublishVolume", request.VolumeId, request.NodeId)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Controller unpublish the volume to remove the ACL
	if err := driver.controllerUnpublishVolume(request.VolumeId, request.NodeId, request.Secrets); err != nil {
		log.Errorf("Failed to controller unpublish volume %s, err: %s", request.VolumeId, err.Error())
		return nil, err
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (driver *Driver) controllerUnpublishVolume(volumeID string, nodeID string, secrets map[string]string) error {
	log.Tracef(">>>>> controllerUnpublishVolume, volumeID: %s, nodeID: %s", volumeID, nodeID)
	defer log.Trace("<<<<< controllerUnpublishVolume")

	// Check if volume is requested with RWX or ROX modes with NFS services and intercept here
	if driver.flavor.IsNFSVolume(volumeID) {
		// TODO: check and add client ACL here
		log.Info("ControllerUnpublish requested with multi-node access-mode, returning success")
		return nil
	}

	// Get Storage Provider
	storageProvider, err := driver.GetStorageProvider(secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		// codes.Internal is treated as Final error and controllerUnpublish returns without another retry.
		// the list of retry able errors are listed at
		// https://github.com/kubernetes-csi/external-attacher/blob/ad7d376eb4ac985860e37903590ce6a17b340b06/pkg/attacher/attacher.go#L117
		return status.Error(codes.Unavailable,
			fmt.Sprintf("Failed to get storage provider from secrets, err: %s", err.Error()))
	}

	// Get Volume
	existingVolume, err := driver.GetVolumeByID(volumeID, secrets)
	if err != nil {
		// Check if codes.NotFound is returned from storage provider, which means the volume is already removed from the backend.
		// Let volumeattachment detach to go through by returning a success for unpublish
		if status.Code(err) == codes.NotFound {
			log.Debugf("Could not find volume with ID %s", volumeID)
			return nil
		}
		log.Errorf("Failed to get volume %s, err: %s", volumeID, err.Error())
		return err
	}

	// pass the nodeID to the container storage provider and do not do a look up of the node object as
	// node object may have been deleted as part of UnloadNodeInfo when the node went down
	if !existingVolume.Published {
		log.Infof("Volume %s is already unpublished from node with ID %s", existingVolume.Name, nodeID)
		return nil
	}

	// Remove ACL from the volume based on the requested Node ID
	err = storageProvider.UnpublishVolume(existingVolume.ID, nodeID)
	if err != nil {
		log.Trace("err: ", err.Error())
		return status.Error(codes.Aborted,
			fmt.Sprintf("Failed to remove ACL via CSP, err: %s", err.Error()))
	}
	return nil
}

// ValidateVolumeCapabilities ...
//
// A Controller Plugin MUST implement this RPC call. This RPC will be called by the CO to check if a pre-provisioned volume has all the capabilities
// that the CO wants. This RPC call SHALL return confirmed only if all the volume capabilities specified in the request are supported (see caveat
// below). This operation MUST be idempotent.
//
// NOTE: Older plugins will parse but likely not "process" newer fields that MAY be present in capability-validation messages (and sub-messages)
// sent by a CO that is communicating using a newer, backwards-compatible version of the CSI protobufs. Therefore, the CO SHALL reconcile successful
// capability-validation responses by comparing the validated capabilities with those that it had originally requested.
// nolint: gocyclo
func (driver *Driver) ValidateVolumeCapabilities(ctx context.Context, request *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	log.Trace(">>>>> ValidateVolumeCapabilities")
	defer log.Trace("<<<<< ValidateVolumeCapabilities")

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided to ValidateVolumeCapabilities")
	}

	if request.VolumeCapabilities == nil || len(request.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities are required to ValidateVolumeCapabilities")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s", "ValidateVolumeCapabilities", request.VolumeId)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Check if the volume exists using Secrets
	_, err := driver.GetVolumeByID(request.VolumeId, request.Secrets)
	if err != nil {
		log.Error("Failed to get volume ", request.VolumeId)
		return nil, err
	}

	// Check if the requested volcapabilities are supported
	_, err = driver.AreVolumeCapabilitiesSupported(request.VolumeCapabilities)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Found one or more unsupported volume capabilities: %+v", request.VolumeCapabilities))
	}

	// Validate and Get volume access type
	_, err = driver.ValidateAndGetVolumeAccessType(request.VolumeCapabilities)
	if err != nil {
		log.Errorf("Failed to validate and retrieve volume access type, err: %v", err.Error())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to validate and retrieve volume access type, %v", err.Error()))
	}

	// TODO: Do we support pre-provisioned volumes ???

	return &csi.ValidateVolumeCapabilitiesResponse{}, nil
}

// ListVolumes ...
//
// A Controller Plugin MUST implement this RPC call if it has LIST_VOLUMES capability. The Plugin SHALL return the information about all the
// volumes that it knows about. If volumes are created and/or deleted while the CO is concurrently paging through ListVolumes results then it
// is possible that the CO MAY either witness duplicate volumes in the list, not witness existing volumes, or both. The CO SHALL NOT expect a
// consistent "view" of all volumes when paging through the volume list via multiple calls to ListVolumes.
func (driver *Driver) ListVolumes(ctx context.Context, request *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	log.Trace(">>>>> ListVolumes")
	defer log.Trace("<<<<< ListVolumes")

	// TODO: use the paging properties in the request
	// request.StartingToken <- use me
	// request.MaxEntries <- use me

	// TODO: ListVolumes does not provide Secrets nor a NodeId so it simply needs to return information about all of the
	// volumes it knows about.  This can be implemented by reading all volumes from every array known to the driver.  We can do this
	// at initialization time and keep them in memory.
	var allVolumes []*model.Volume
	for _, storageProvider := range driver.storageProviders {
		// TODO: skip storage providers that are not of the same vendor... or not.  Need to think through this
		volumes, err := storageProvider.GetVolumes()
		if err != nil {
			log.Trace("err: ", err.Error())
			return nil, status.Error(codes.Unavailable, "Error while attempting to list volumes")
		}
		for _, volume := range volumes {
			allVolumes = append(allVolumes, volume)
		}
	}

	log.Tracef("get volumes returned: %#v", allVolumes)

	var entries []*csi.ListVolumesResponse_Entry
	for _, vol := range allVolumes {
		entries = append(entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId: vol.ID,
			},
		})
	}

	return &csi.ListVolumesResponse{
		Entries: entries,
	}, nil
}

// ControllerGetVolume ...
//
// ALPHA FEATURE
//
// This optional RPC MAY be called by the CO to fetch current information about a volume.  A Controller Plugin MUST implement this
// ControllerGetVolume RPC call if it has GET_VOLUME capability.  A Controller Plugin MUST provide a non-empty volume_condition field in
// ControllerGetVolumeResponse if it has VOLUME_CONDITION capability.
// ControllerGetVolumeResponse should contain current information of a volume if it exists. If the volume does not exist any more,
// ControllerGetVolume should return gRPC error code NOT_FOUND
func (driver *Driver) ControllerGetVolume(ctx context.Context, request *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	log.Trace(">>>>> ControllerGetVolume")
	defer log.Trace("<<<<< ControllerGetVolume")

	// TODO: Implement this

	return nil, status.Error(codes.Unimplemented, "")
}

// GetCapacity ...
//
// A Controller Plugin MUST implement this RPC call if it has GET_CAPACITY controller capability. The RPC allows the CO to query the capacity of
// the storage pool from which the controller provisions volumes.
// nolint: dupl
func (driver *Driver) GetCapacity(ctx context.Context, request *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	log.Trace(">>>>> GetCapacity")
	defer log.Trace("<<<<< GetCapacity")

	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities ...
//
// A Controller Plugin MUST implement this RPC call. This RPC allows the CO to check the supported capabilities of controller service provided
// by the Plugin.
// nolint: dupl
func (driver *Driver) ControllerGetCapabilities(ctx context.Context, request *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	log.Trace(">>>>> ControllerGetCapabilities")
	defer log.Trace("<<<<< ControllerGetCapabilities")

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: driver.controllerServiceCapabilities,
	}, nil
}

// CreateSnapshot ...
//
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_SNAPSHOT controller capability. This RPC will be called by the CO to
// create a new snapshot from a source volume on behalf of a user.
//
// This operation MUST be idempotent. If a snapshot corresponding to the specified snapshot name is successfully cut and ready to use (meaning it
// MAY be specified as a volume_content_source in a CreateVolumeRequest), the Plugin MUST reply 0 OK with the corresponding CreateSnapshotResponse.
//
// If an error occurs before a snapshot is cut, CreateSnapshot SHOULD return a corresponding gRPC error code that reflects the error condition.
//
// For plugins that supports snapshot post processing such as uploading, CreateSnapshot SHOULD return 0 OK and ready_to_use SHOULD be set to false
// after the snapshot is cut but still being processed. CO SHOULD then reissue the same CreateSnapshotRequest periodically until boolean
// ready_to_use flips to true indicating the snapshot has been "processed" and is ready to use to create new volumes. If an error occurs during the
// process, CreateSnapshot SHOULD return a corresponding gRPC error code that reflects the error condition.
//
// A snapshot MAY be used as the source to provision a new volume. A CreateVolumeRequest message MAY specify an OPTIONAL source snapshot parameter.
// Reverting a snapshot, where data in the original volume is erased and replaced with data in the snapshot, is an advanced functionality not every
// storage system can support and therefore is currently out of scope.
// nolint: dupl
func (driver *Driver) CreateSnapshot(ctx context.Context, request *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	log.Trace(">>>>> CreateSnapshot")
	defer log.Trace("<<<<< CreateSnapshot")

	log.Infof("CreateSnapshot requested snapshot '%s' for volume '%s' with the following Parameters: '%v'", request.Name, request.SourceVolumeId, request.Parameters)

	// Name
	if request.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name must be provided to create a snapshot")
	}

	// Source Volume ID
	if request.SourceVolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Source volume ID must be provided to create a snapshot")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s:%s", "CreateSnapshot", request.Name, request.SourceVolumeId)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// Create DB entry
	dbKey := request.Name // TODO: Is this unique. If not, then we might need to append sourceVolumeId
	if err := driver.AddToDB(dbKey, Pending); err != nil {
		return nil, err
	}
	defer driver.RemoveFromDBIfPending(key)

	// Secrets
	storageProvider, err := driver.GetStorageProvider(request.Secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to get storage provider from secrets, %s", err.Error()))
	}

	// check if volume-id is fake(for nfs) and obtain real backing volume-id
	nfsVolumeID, err := driver.flavor.GetNFSVolumeID(request.SourceVolumeId)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to check if volume-id %s is of nfs type, %s", request.SourceVolumeId, err.Error()))
	}

	if nfsVolumeID != "" {
		// replace requested volume-id(NFS) with real volume-id(RWO PV)
		request.SourceVolumeId = nfsVolumeID
	}

	// Check if the source volume exists using Secrets
	_, err = driver.GetVolumeByID(request.SourceVolumeId, request.Secrets)
	if err != nil {
		log.Error("Failed to get source volume ", request.SourceVolumeId)
		return nil, err
	}

	// Pass the snapshot parameters to the CSP to handle them accordingly if supported.
	createOptions := make(map[string]interface{})
	for k, v := range request.Parameters {
		// convert create options to snakecase before passing it to CSP
		createOptions[util.ToSnakeCase(k)] = v
	}
	log.Trace("Snapshot create options: ", createOptions)

	// extract the description if present
	var description string
	if val, ok := createOptions[descriptionKey]; ok {
		description = fmt.Sprintf("%v", val)
		delete(createOptions, descriptionKey)
	}

	// Check if snapshot with the name for the given source ID already exists
	existingSnapshot, err := storageProvider.GetSnapshotByName(request.Name, request.SourceVolumeId)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to check if snapshot %s exists for volume %s", request.Name, request.SourceVolumeId))
	}

	if existingSnapshot != nil {
		log.Tracef("GetSnapshot(name) returned: %#v", existingSnapshot)
		log.Tracef("Snapshot with name %s and ID %s already exists. Returning it.", existingSnapshot.Name, existingSnapshot.ID)

		// Snapshot with name already exists but is incompatible with the specified volume_id
		if existingSnapshot.VolumeID != request.SourceVolumeId {
			return nil, status.Error(codes.AlreadyExists,
				fmt.Sprintf("Snapshot corresponding to the specified snapshot name %s already exists but is incompatible with the specified volume id %s",
					request.Name, request.SourceVolumeId))
		}

		// Update DB entry
		if err := driver.UpdateDB(dbKey, existingSnapshot); err != nil {
			return nil, err
		}

		return &csi.CreateSnapshotResponse{
			Snapshot: &csi.Snapshot{
				SnapshotId:     existingSnapshot.ID,
				SourceVolumeId: existingSnapshot.VolumeID,
				SizeBytes:      existingSnapshot.Size,
				CreationTime:   convertSecsToTimestamp(existingSnapshot.CreationTime),
				ReadyToUse:     existingSnapshot.ReadyToUse,
			},
		}, nil
	}

	/*
		TODO: Handle below failure cases and return appropriate error code
		   - Operation pending for snapshot (snapshot request already in flight)
		   - Not enough space to create snapshot
	*/

	// Create a new snapshot
	snapshot, err := storageProvider.CreateSnapshot(request.Name, description, request.SourceVolumeId, createOptions)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to create snapshot %s for volume %s, err: %s", request.Name, request.SourceVolumeId, err.Error()))
	}
	log.Tracef("Snapshot: %#v", snapshot)
	log.Tracef("Created new snapshot %s from source volume %s (Name: %s) successfully", snapshot.Name, snapshot.VolumeID, snapshot.VolumeName)

	// Update DB entry
	if err := driver.UpdateDB(dbKey, snapshot); err != nil {
		return nil, err
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     snapshot.ID,
			SourceVolumeId: snapshot.VolumeID,
			SizeBytes:      snapshot.Size,
			CreationTime:   convertSecsToTimestamp(snapshot.CreationTime),
			ReadyToUse:     snapshot.ReadyToUse,
		},
	}, nil
}

// DeleteSnapshot ...
//
// A Controller Plugin MUST implement this RPC call if it has CREATE_DELETE_SNAPSHOT capability. This RPC will be called by the CO to delete a
// snapshot.
//
// This operation MUST be idempotent. If a snapshot corresponding to the specified snapshot_id does not exist or the artifacts associated with
// the snapshot do not exist anymore, the Plugin MUST reply 0 OK.
// nolint: dupl
func (driver *Driver) DeleteSnapshot(ctx context.Context, request *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	log.Trace(">>>>> DeleteSnapshot")
	defer log.Trace("<<<<< DeleteSnapshot")

	if request.SnapshotId == "" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot ID must be provided for DeleteSnapshot")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s", "DeleteSnapshot", request.SnapshotId)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	log.Infof("DeleteSnapshot requested snapshot %s for deletion", request.SnapshotId)

	// Secrets
	storageProvider, err := driver.GetStorageProvider(request.Secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to get storage provider from secrets, %s", err.Error()))
	}

	// Get Snapshot using secrets
	existingSnapshot, err := storageProvider.GetSnapshot(request.SnapshotId)
	if err != nil {
		// Error while retrieving the snapshot using id and secrets
		return nil, status.Error(codes.Internal, fmt.Sprintf("Error while attempting to get snapshot with ID %s", request.SnapshotId))
	}
	if existingSnapshot == nil {
		log.Tracef("Snapshot %s does not exist", request.SnapshotId)
		return &csi.DeleteSnapshotResponse{}, nil
	}
	log.Tracef("GetSnapshot(id) returned: %#v", existingSnapshot)

	// Check if snapshot is still in use. (May be due to snapshot having clones)
	if existingSnapshot.InUse {
		return nil, status.Error(codes.FailedPrecondition,
			fmt.Sprintf("The snapshot corresponding to the specified snapshot ID %s could not be deleted because it is in use by another resource", existingSnapshot.ID))
	}

	// Delete the snapshot from the array
	if err := storageProvider.DeleteSnapshot(request.SnapshotId); err != nil {
		return nil, status.Error(codes.Internal, "Error while deleting snapshot "+request.SnapshotId)
	}

	// Delete DB entry
	if err := driver.RemoveFromDB(existingSnapshot.Name); err != nil {
		return nil, err
	}

	return &csi.DeleteSnapshotResponse{}, nil
}

// ListSnapshots ...
//
// A Controller Plugin MUST implement this RPC call if it has LIST_SNAPSHOTS capability. The Plugin SHALL return the information about all
// snapshots on the storage system within the given parameters regardless of how they were created. ListSnapshots SHALL NOT list a snapshot
// that is being created but has not been cut successfully yet. If snapshots are created and/or deleted while the CO is concurrently paging
// through ListSnapshots results then it is possible that the CO MAY either witness duplicate snapshots in the list, not witness existing
// snapshots, or both. The CO SHALL NOT expect a consistent "view" of all snapshots when paging through the snapshot list via multiple calls
// to ListSnapshots.
// nolint: dupl
func (driver *Driver) ListSnapshots(ctx context.Context, request *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	log.Trace(">>>>> ListSnapshots")
	defer log.Trace("<<<<< ListSnapshots")

	// TODO: use the paging properties in the request
	// request.StartingToken <- use me
	// request.MaxEntries <- use me

	var allSnapshots []*model.Snapshot

	// Secrets are optional
	if request.Secrets != nil {
		storageProvider, err := driver.GetStorageProvider(request.Secrets)
		if err != nil {
			log.Error("err: ", err.Error())
			return nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to get storage provider from secrets, %s", err.Error()))
		}
		allSnapshots, err = getSnapshotsForStorageProvider(ctx, request, storageProvider)
		if err != nil {
			return nil, err
		}
	} else {
		// In the event that ListSnapshots does not provide Secrets it simply needs to return information about all of the
		// snapshots it knows about.  This can be implemented by reading all snapshots from every array known to the driver.  We can do this
		// at initialization time and keep them in memory.

		// If driver doesn't know any storage providers yet, then return empty list with success
		if len(driver.storageProviders) == 0 {
			log.Info("No storage providers are known to the CSI driver yet.")
			return &csi.ListSnapshotsResponse{
				Entries: []*csi.ListSnapshotsResponse_Entry{},
			}, nil
		}

		for _, storageProvider := range driver.storageProviders {
			snapshots, err := getSnapshotsForStorageProvider(ctx, request, storageProvider)
			if err != nil {
				return nil, err
			}

			if len(snapshots) > 0 {
				for _, snapshot := range snapshots {
					allSnapshots = append(allSnapshots, snapshot)
				}
				// Snapshots found on the storage provider associated with the volume or snapshot ID.  No need to check the next one
				break
			}
		}
	}
	log.Tracef("Total snapshots: %d", len(allSnapshots))

	var entries []*csi.ListSnapshotsResponse_Entry
	for _, snapshot := range allSnapshots {
		entries = append(entries, &csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SnapshotId:     snapshot.ID,
				SourceVolumeId: snapshot.VolumeID,
				SizeBytes:      snapshot.Size,
				CreationTime:   convertSecsToTimestamp(snapshot.CreationTime),
				ReadyToUse:     snapshot.ReadyToUse,
			},
		})
	}

	return &csi.ListSnapshotsResponse{
		Entries: entries,
	}, nil
}

// ControllerExpandVolume ...
//
// A Controller plugin MUST implement this RPC call if plugin has EXPAND_VOLUME controller capability. This RPC allows the CO to expand the size
// of a volume.
//
// This call MAY be made by the CO during any time in the lifecycle of the volume after creation if plugin has VolumeExpansion.ONLINE capability.
// If plugin has EXPAND_VOLUME node capability, then NodeExpandVolume MUST be called after successful ControllerExpandVolume and
// node_expansion_required in ControllerExpandVolumeResponse is true.
//
// If the plugin has only VolumeExpansion.OFFLINE expansion capability and volume is currently published or available on a node then
// ControllerExpandVolume MUST be called ONLY after either:
//     The plugin has controller PUBLISH_UNPUBLISH_VOLUME capability and ControllerUnpublishVolume has been invoked successfully.
// OR ELSE
//     The plugin does NOT have controller PUBLISH_UNPUBLISH_VOLUME capability, the plugin has node STAGE_UNSTAGE_VOLUME capability, and
//     NodeUnstageVolume has been completed successfully.
// OR ELSE
//     The plugin does NOT have controller PUBLISH_UNPUBLISH_VOLUME capability, nor node STAGE_UNSTAGE_VOLUME capability, and
//     NodeUnpublishVolume has completed successfully.
// nolint: dupl
func (driver *Driver) ControllerExpandVolume(ctx context.Context, request *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	log.Trace(">>>>> ControllerExpandVolume")
	defer log.Trace("<<<<< ControllerExpandVolume")

	if request.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided for ControllerExpandVolume")
	}

	if request.GetCapacityRange() == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range must be provided for ControllerExpandVolume")
	}

	if request.CapacityRange.GetRequiredBytes() == 0 && request.CapacityRange.GetLimitBytes() == 0 {
		return nil, status.Error(codes.InvalidArgument, "Either required_bytes or limit_bytes must be provided for ControllerExpandVolume")
	}

	if request.CapacityRange.GetRequiredBytes() < 0 || request.CapacityRange.GetLimitBytes() < 0 {
		return nil, status.Error(codes.InvalidArgument, "Either required_bytes or limit_bytes are provided with negative values for ControllerExpandVolume")
	}

	if request.CapacityRange.GetLimitBytes() != 0 && request.CapacityRange.GetRequiredBytes() > request.CapacityRange.GetLimitBytes() {
		return nil, status.Error(codes.InvalidArgument, "required_bytes is greater than limit_bytes for ControllerExpandVolume")
	}

	// Check for duplicate request. If yes, then return ABORTED
	key := fmt.Sprintf("%s:%s", "ControllerExpandVolume", request.VolumeId)
	if err := driver.HandleDuplicateRequest(key); err != nil {
		return nil, err // ABORTED
	}
	defer driver.ClearRequest(key)

	// TODO: Add info to DB

	// Get Volume
	existingVolume, err := driver.GetVolumeByID(request.VolumeId, request.Secrets)
	if err != nil {
		log.Error("Failed to get volume with ID ", request.VolumeId)
		return nil, err
	}
	log.Tracef("Found Volume %s with ID %s", existingVolume.Name, existingVolume.ID)

	// Set node expansion required to 'true' for mount type as fs resize is always required in that case
	nodeExpansionRequired := true
	if request.GetVolumeCapability() != nil {
		switch request.GetVolumeCapability().GetAccessType().(type) {
		case *csi.VolumeCapability_Block:
			if !existingVolume.Published {
				log.Info("node expansion is not required for raw block volumes when not published")
				nodeExpansionRequired = false
			}
		}
	}

	if existingVolume.Size == request.CapacityRange.GetLimitBytes() || existingVolume.Size == request.CapacityRange.GetRequiredBytes() {
		// volume is already at requested size, so no action required.
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         existingVolume.Size,
			NodeExpansionRequired: nodeExpansionRequired,
		}, nil
	}

	// check if online expansion capability is supported when published volume expand is requested.
	if existingVolume.Published && !driver.IsSupportedPluginVolumeExpansionCapability(csi.PluginCapability_VolumeExpansion_ONLINE) {
		return nil, status.Error(codes.FailedPrecondition,
			fmt.Sprintf("Volume %s could not be expanded because it is currently published on a node but the plugin does not have ONLINE expansion capability",
				existingVolume.ID))
	}

	// Get storage provider
	storageProvider, err := driver.GetStorageProvider(request.Secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable,
			fmt.Sprintf("Failed to get storage provider from secrets, err: %s", err.Error()))
	}

	var updatedVolume *model.Volume
	// attempt resize as requested
	if request.CapacityRange.GetRequiredBytes() != 0 {
		log.Tracef("attempt to expand volume to size %d bytes", request.CapacityRange.GetRequiredBytes())
		updatedVolume, err = storageProvider.ExpandVolume(request.VolumeId, request.CapacityRange.GetRequiredBytes())
	} else {
		log.Tracef("attempt to expand volume to size %d bytes", request.CapacityRange.GetLimitBytes())
		updatedVolume, err = storageProvider.ExpandVolume(request.VolumeId, request.CapacityRange.GetLimitBytes())
	}
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to expand volume to requested size, %s", err.Error()))
	}

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         updatedVolume.Size,
		NodeExpansionRequired: nodeExpansionRequired,
	}, nil
}

func updateVolumeContext(volumeContext map[string]string, volume *model.Volume) error {
	for k, v := range volume.Config {
		switch value := v.(type) {
		case string:
			volumeContext[util.ToCamelCase(k)] = value
		default:
			volumeContext[util.ToCamelCase(k)] = fmt.Sprintf("%v", value)
		}
	}
	return nil
}

// Returns all snapshots for the given storage provider given the list request
func getSnapshotsForStorageProvider(ctx context.Context, request *csi.ListSnapshotsRequest, storageProvider storageprovider.StorageProvider) ([]*model.Snapshot, error) {
	log.Trace(">>>>> getSnapshotsForStorageProvider")
	defer log.Trace("<<<<< getSnapshotsForStorageProvider")

	var allSnapshots []*model.Snapshot

	if request.SnapshotId != "" {
		snapshot, err := storageProvider.GetSnapshot(request.SnapshotId)
		if err != nil {
			// Error while retrieving the snapshot using id and secrets
			return nil, status.Error(codes.Internal, fmt.Sprintf("Error while attempting to get snapshot with ID %s", request.SnapshotId))
		}
		if snapshot != nil {
			allSnapshots = append(allSnapshots, snapshot)
		}
	} else if request.SourceVolumeId != "" {
		snapshots, err := storageProvider.GetSnapshots(request.SourceVolumeId)
		if err != nil {
			log.Trace("err: ", err.Error())
			return nil, status.Error(codes.Internal, "Error while attempting to list snapshots")
		}
		log.Tracef("Read %d snapshots from a storage provider", len(snapshots))
		for _, snapshot := range snapshots {
			allSnapshots = append(allSnapshots, snapshot)
		}
	}

	return allSnapshots, nil
}
