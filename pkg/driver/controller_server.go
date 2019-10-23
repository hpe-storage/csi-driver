// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"fmt"
	"strconv"
	"strings"

	csi "github.com/container-storage-interface/spec/lib/go/csi/v0"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
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

	// Check if the requested volume capabilities are supported.
	_, err := driver.AreVolumeCapabilitiesSupported(request.VolumeCapabilities)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Found one or more unsupported volume capabilities: %+v",
				request.VolumeCapabilities))
	}

	// Validate and Get volume access type
	volAccessType, err := driver.ValidateAndGetVolumeAccessType(request.VolumeCapabilities)
	if err != nil {
		log.Errorf("Failed to validate and retrieve volume access type, err: %v", err.Error())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to validate and retrieve volume access type, %v", err.Error()))
	}

	// Secrets
	storageProvider, err := driver.GetStorageProvider(request.ControllerCreateSecrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Internal, "Failed to get storage provider from secrets")
	}

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

	// TODO: use additional properties here to configure the volume further... these might come in from doryd
	createOptions := make(map[string]interface{})
	for k, v := range createParameters {
		// convert create options to snakecase before passing it to CSP
		createOptions[util.ToSnakeCase(k)] = v
	}
	log.Trace("Volume create options: ", createOptions)

	// extract the description if present
	var description string
	if val, ok := createOptions[descriptionKey]; ok {
		description = fmt.Sprintf("%v", val)
		delete(createOptions, descriptionKey)
	}

	// The volume access type in the request will be either 'Block' or 'Mount'
	// If none, then we default to 'Mount' access type and use default filesystem.
	var filesystem string
	if volAccessType == model.MountType {
		foundFsType := false
		// Only one FS type must be specified in the volcaps list.
		// If more than one is requested, then return appropriate error.
		for _, cap := range request.VolumeCapabilities {
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
	}

	// Build the volume context to be returned to the CO in the create response
	// This same context will be used by the CO during Publish and Stage workflows
	respVolContext := map[string]string{}
	if request.Parameters != nil {
		// Copy the request parameters into resp context
		respVolContext = request.Parameters

		// For block access, the filesystem will be empty.
		if filesystem != "" {
			log.Trace("Adding filesystem to the volume context, Filesystem: ", filesystem)
			respVolContext[filesystemType] = filesystem
		}
	}
	log.Trace("Volume context in response to CO: ", respVolContext)

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
	existingVolume, err := storageProvider.GetVolumeByName(request.Name)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Internal, "Failed to check if volume exists")
	}

	if existingVolume != nil {
		log.Trace("Volume found :", existingVolume.Name)
		log.Trace("existingVolume.id :", existingVolume.ID)
		log.Trace("existingVolume.Size :", existingVolume.Size)

		// Check if the existing volume's size matches with the requested size
		// TODO: Nimble doesn't support capacity range, but other SP might.
		// We may consider adding range support in the future???
		if existingVolume.Size != reqVolumeSize {
			log.Trace("Requested size :", reqVolumeSize)
			return nil, status.Error(
				codes.AlreadyExists,
				fmt.Sprintf("Volume %s with different size %d already exists", request.Name, existingVolume.Size))
		}

		// Check if the source content is being requested.
		if request.VolumeContentSource != nil {
			log.Tracef("Requested volumeContentSource, %+v", request.VolumeContentSource)
			return nil, status.Error(
				codes.AlreadyExists,
				fmt.Sprintf("Volume %s already exists, but the source content is being requested", request.Name))
		}

		// Update DB entry
		if err := driver.UpdateDB(dbKey, existingVolume); err != nil {
			return nil, err
		}

		// Return existing volume
		log.Tracef("Returning the existing volume '%s' with size %d", existingVolume.Name, existingVolume.Size)
		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				Id:            existingVolume.ID,
				CapacityBytes: existingVolume.Size,
			},
		}, nil
	}

	// TODO: If volume already exists and volumeContentSource is specified, should CSI throw error ???

	// VolumeContentSource
	if request.VolumeContentSource != nil {
		log.Tracef("Requested VolumeContentSource : %+v", request.VolumeContentSource)

		// Verify source snap if specified
		if request.VolumeContentSource.GetSnapshot() != nil {
			// Check if the driver supports SNAPSHOT service
			if !driver.IsSupportedControllerCapability(csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT) {
				return nil, status.Error(codes.InvalidArgument, "Unable to create volume from snapshot. Driver does not support snapshot create/delete")
			}

			snapID := request.VolumeContentSource.GetSnapshot().Id
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
			log.Tracef("GetSnapshot returned: %#v", existingSnap)

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
						fmt.Sprintf("Failed to check if snapshot's parent volume %s with ID %s exist", existingSnap.VolumeName, existingSnap.VolumeID))
			}
			// The requested size is must be at least equal to the snapshot's parent volume size
			if reqVolumeSize < existingParentVolume.Size {
				return nil,
					status.Error(codes.InvalidArgument,
						fmt.Sprintf("Requested volume size %d cannot be lesser than snapshot's parent volume size %d", reqVolumeSize, existingParentVolume.Size))
			}

			// Create a clone from another volume
			log.Infof("About to create a new clone '%s' with size %d from snapshot %s with options %+v", request.Name, reqVolumeSize, existingSnap.ID, createOptions)
			volume, err := storageProvider.CloneVolume(request.Name, description, "", existingSnap.ID, reqVolumeSize, createOptions)
			if err != nil {
				log.Trace("err: " + err.Error())
				return nil, status.Error(codes.Internal, "Failed to create volume due to error: "+err.Error())
			}

			// Update DB entry
			if err := driver.UpdateDB(dbKey, volume); err != nil {
				return nil, err
			}

			// Return newly cloned volume (clone from snapshot)
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					Id:            volume.ID,
					CapacityBytes: volume.Size,
					Attributes:    respVolContext,
				},
			}, nil
		}

		return nil, status.Error(codes.InvalidArgument, "One of snapshot or parent volume source must be specified")
	}

	// Create new volume
	log.Infof("About to create a new volume '%s' with size %d and options %+v", request.Name, reqVolumeSize, createOptions)
	volume, err := storageProvider.CreateVolume(request.Name, description, reqVolumeSize, createOptions)
	if err != nil {
		log.Trace("err: " + err.Error())
		return nil, status.Error(codes.Internal, "Failed to create volume due to error: "+err.Error())
	}

	// Update DB entry
	if err := driver.UpdateDB(dbKey, volume); err != nil {
		return nil, err
	}

	// Return newly created volume
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			Id:            volume.ID,
			CapacityBytes: volume.Size,
			Attributes:    respVolContext,
		},
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

	log.Infof("DeleteVolume requested volume %s for deletion", request.VolumeId)

	// Get Volume using secrets
	existingVolume, err := driver.GetVolumeByID(request.VolumeId, request.ControllerDeleteSecrets)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			log.Trace("Could not find volume with ID " + request.VolumeId)
			return &csi.DeleteVolumeResponse{}, nil
		}
		// Error while retrieving the volume using id and secrets
		return nil, err
	}
	log.Tracef("Found Volume %s with ID %s", existingVolume.Name, existingVolume.ID)

	// Check if volume is busy or in-use
	if existingVolume.InUse {
		log.Errorf("Volume %s with ID %s still in use", existingVolume.Name, existingVolume.ID)
		return nil, status.Errorf(codes.FailedPrecondition, fmt.Sprintf("Volume %s with ID %s is still in use", existingVolume.Name, existingVolume.ID))
	}

	// Get Storage Provider
	storageProvider, err := driver.GetStorageProvider(request.ControllerDeleteSecrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Internal, "Failed to get storage provider from secrets")
	}

	// Delete the volume from the array
	if err := storageProvider.DeleteVolume(request.VolumeId, false); err != nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("Error while deleting volume %s, err: %s", existingVolume.Name, err.Error()))
	}

	// Delete DB entry
	if err := driver.RemoveFromDB(existingVolume.Name); err != nil {
		return nil, err
	}

	return &csi.DeleteVolumeResponse{}, nil
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

	// Read-Only
	// Indicates SP MUST publish the volume in readonly mode.
	// CO MUST set this field to false if SP does not have the PUBLISH_READONLY controller capability.
	// This is a REQUIRED field
	if request.Readonly {
		// Publish the volume in read-only mode if set to true
		log.Tracef("Volume %s must be published in READONLY mode", request.VolumeId)
	}

	log.Infof("ControllerPublishVolume requested volume %s and node %s with capability %v and context %v",
		request.VolumeId, request.NodeId, request.VolumeCapability, request.VolumeAttributes)

	// Validate Capability
	log.Tracef("Validating volume capability: %+v", request.VolumeCapability)
	_, err := driver.IsValidVolumeCapability(request.VolumeCapability)
	if err != nil {
		log.Errorf("Found unsupported volume capability %+v", request.VolumeCapability)
		return nil, err
	}

	// Get volume access type
	volAccessType, err := driver.getVolumeAccessType(request.VolumeCapability)
	if err != nil {
		log.Errorf("Failed to retrieve volume access type, err: %v", err.Error())
		return nil, status.Error(codes.InvalidArgument,
			fmt.Sprintf("Failed to retrieve volume access type, %v", err.Error()))
	}

	// Get Storage Provider
	storageProvider, err := driver.GetStorageProvider(request.ControllerPublishSecrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Internal, "Failed to get storage provider from secrets")
	}

	// Get Volume
	existingVolume, err := driver.GetVolumeByID(request.VolumeId, request.ControllerPublishSecrets)
	if err != nil {
		log.Error("Failed to get volume ", request.VolumeId)
		return nil, err
	}
	log.Tracef("Found Volume %s with ID %s", existingVolume.Name, existingVolume.ID)

	// Decode and check if the node is configured
	node, err := driver.flavor.GetNodeInfo(request.NodeId)
	if err != nil {
		log.Error("Cannot unmarshal node from node ID. Error: ", err.Error())
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// check if the node context is already available in the CSP
	existingNode, err := storageProvider.GetNodeContext(node.UUID)
	if err != nil {
		log.Error("Error retrieving the node info from the CSP. Error: ", err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}
	if existingNode != nil {
		log.Tracef("CSP has already been notified about the node with ID %s and UUID %s", existingNode.ID, existingNode.UUID)
	} else {
		// If node does not already exists, then set context here
		log.Tracef("Notifying CSP about Node with ID %s and UUID %s", node.ID, node.UUID)
		if err = storageProvider.SetNodeContext(node); err != nil {
			log.Error("err: ", err.Error())
			return nil, status.Error(codes.Internal, "Failed to provide node context to CSP")
		}
	}

	// Configure access protocol defaulting to iSCSI when unspecified
	var requestedAccessProtocol = request.VolumeAttributes[accessProtocol]
	if requestedAccessProtocol == "" {
		if len(node.Iqns) != 0 {
			requestedAccessProtocol = iscsi
		} else {
			requestedAccessProtocol = fc
		}
	}

	// Add ACL to the volume based on the requested Node ID
	publishInfo, err := storageProvider.PublishVolume(request.VolumeId, node.UUID, requestedAccessProtocol)
	if err != nil {
		log.Trace("err: ", err.Error())
		return nil, status.Error(codes.Internal, "Failed to add ACL via container provider")
	}
	log.Tracef("PublishInfo response from CSP: %v", publishInfo)

	// target scope is nimble specific therefore extract it from the volume config
	var requestedTargetScope = targetScopeGroup
	if val, ok := existingVolume.Config[targetScope]; ok {
		requestedTargetScope = fmt.Sprintf("%v", val)
	}

	// TODO: add any additional info necessary to mount the device
	publishContext := map[string]string{}
	publishContext[serialNumber] = publishInfo.SerialNumber
	publishContext[accessProtocol] = publishInfo.AccessInfo.BlockDeviceAccessInfo.AccessProtocol
	publishContext[targetName] = publishInfo.AccessInfo.BlockDeviceAccessInfo.TargetName
	publishContext[targetScope] = requestedTargetScope
	publishContext[lunID] = strconv.Itoa(int(publishInfo.AccessInfo.BlockDeviceAccessInfo.LunID))
	if strings.EqualFold(publishInfo.AccessInfo.BlockDeviceAccessInfo.AccessProtocol, iscsi) {
		publishContext[discoveryIPs] = strings.Join(publishInfo.AccessInfo.BlockDeviceAccessInfo.IscsiAccessInfo.DiscoveryIPs, ",")
		publishContext[chapUsername] = publishInfo.AccessInfo.BlockDeviceAccessInfo.IscsiAccessInfo.ChapUser
		publishContext[chapPassword] = publishInfo.AccessInfo.BlockDeviceAccessInfo.IscsiAccessInfo.ChapPassword
	}
	publishContext[readOnly] = strconv.FormatBool(request.Readonly)
	publishContext[volumeAccessMode] = volAccessType.String() // Block or Mount

	// Publish FS details only if 'mount' access is being requested
	if volAccessType == model.MountType {
		// Filesystem Details
		log.Trace("Adding filesystem details to the publish context")
		publishContext[filesystemType] = request.VolumeAttributes[filesystemType]
		publishContext[filesystemOwner] = request.VolumeAttributes[filesystemOwner]
		publishContext[filesystemMode] = request.VolumeAttributes[filesystemMode]
	}

	log.Tracef("Volume %s with ID %s published with the following details: %v",
		existingVolume.Name, existingVolume.ID, publishContext)

	return &csi.ControllerPublishVolumeResponse{
		PublishInfo: publishContext,
	}, nil
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

	log.Infof("ControllerUnpublishVolume requested volume %s and node %s", request.VolumeId, request.NodeId)

	// Decode and get node details
	node, err := driver.flavor.GetNodeInfo(request.NodeId)
	if err != nil {
		return nil, status.Error(codes.Aborted, err.Error())
	}

	// Get Storage Provider
	storageProvider, err := driver.GetStorageProvider(request.ControllerUnpublishSecrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Aborted, "Failed to get storage provider from secrets")
	}

	// Get Volume
	existingVolume, err := driver.GetVolumeByID(request.VolumeId, request.ControllerUnpublishSecrets)
	if err != nil {
		log.Error("Failed to get volume ", request.VolumeId)
		return nil, err
	}
	log.Tracef("Found Volume %s with ID %s", existingVolume.Name, existingVolume.ID)

	// Remove ACL from the volume based on the requested Node ID
	err = storageProvider.UnpublishVolume(request.VolumeId, node.UUID)
	if err != nil {
		log.Trace("err: ", err.Error())
		return nil, status.Error(codes.Aborted, "Failed to remove ACL via container provider")
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
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
	_, err := driver.GetVolumeByID(request.VolumeId, nil)
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
			return nil, status.Error(codes.Internal, "Error while attempting to list volumes")
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
				Id: vol.ID,
			},
		})
	}

	return &csi.ListVolumesResponse{
		Entries: entries,
	}, nil
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
	storageProvider, err := driver.GetStorageProvider(request.CreateSnapshotSecrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Internal, "Failed to get storage provider from secrets")
	}

	// Check if the source volume exists using Secrets
	_, err = driver.GetVolumeByID(request.SourceVolumeId, request.CreateSnapshotSecrets)
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
				Id:             existingSnapshot.ID,
				SourceVolumeId: existingSnapshot.VolumeID,
				SizeBytes:      existingSnapshot.Size,
				CreatedAt:      existingSnapshot.CreationTime * nanos,
				Status: &csi.SnapshotStatus{
					Type: csi.SnapshotStatus_READY,
				},
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
			Id:             snapshot.ID,
			SourceVolumeId: snapshot.VolumeID,
			SizeBytes:      snapshot.Size,
			CreatedAt:      snapshot.CreationTime * nanos,
			Status: &csi.SnapshotStatus{
				Type: csi.SnapshotStatus_READY,
			},
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
	storageProvider, err := driver.GetStorageProvider(request.DeleteSnapshotSecrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Internal, "Failed to get storage provider from secrets")
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

	// If driver doesn't know any storage providers yet, then return empty list with success
	if len(driver.storageProviders) == 0 {
		log.Info("No storage providers are known to the CSI driver yet.")
		return &csi.ListSnapshotsResponse{
			Entries: []*csi.ListSnapshotsResponse_Entry{},
		}, nil
	}

	// TODO: ListSnapshots does not provide Secrets nor a NodeId so it simply needs to return information about all of the
	// snapshots it knows about.  This can be implemented by reading all snapshots from every array known to the driver.  We can do this
	// at initialization time and keep them in memory.
	var allSnapshots []*model.Snapshot
	for _, storageProvider := range driver.storageProviders {
		// TODO: skip storage providers that are not of the same vendor... or not.  Need to think through this
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
	log.Tracef("Total snapshots: %d", len(allSnapshots))

	var entries []*csi.ListSnapshotsResponse_Entry
	for _, snapshot := range allSnapshots {
		entries = append(entries, &csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				Id:             snapshot.ID,
				SourceVolumeId: snapshot.VolumeID,
				SizeBytes:      snapshot.Size,
				CreatedAt:      snapshot.CreationTime * nanos,
				Status: &csi.SnapshotStatus{
					Type: csi.SnapshotStatus_READY,
				},
			},
		})
	}

	return &csi.ListSnapshotsResponse{
		Entries: entries,
	}, nil
}
