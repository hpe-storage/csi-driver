// Copyright 2019 Hewlett Packard Enterprise Development LP

// Note the current implementation below uses the Container Provider.  This is where we will connect to the Container Storage Provider instead.

package csp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/hpe-storage/common-host-libs/concurrent"
	"github.com/hpe-storage/common-host-libs/connectivity"
	"github.com/hpe-storage/common-host-libs/jsonutil"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/storageprovider"
)

const (
	snapshotPrefix = "snap-for-clone-"
	tokenHeader    = "x-auth-token"
	arrayIPHeader  = "x-array-ip"

	descriptionKey   = "description"
	volumeGroupIdKey = "volumeGroupId"
)

var (
	loginMutex = concurrent.NewMapMutex()
)

// ErrorsPayload is a serializer struct for representing a valid JSON API errors payload.
type ErrorsPayload struct {
	Errors []*ErrorObject `json:"errors"`
}

// ErrorObject is an `Error` implementation as well as an implementation of the JSON API error object.
type ErrorObject struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

const (
	// CspClientTimeout : Timeout for a connection to the CSP
	CspClientTimeout = time.Duration(300) * time.Second
)

// ContainerStorageProvider is an implementor of the StorageProvider interface
type ContainerStorageProvider struct {
	Credentials *storageprovider.Credentials

	Client    *connectivity.Client
	AuthToken string
}

// NewContainerStorageProvider is an opportunity to configure the CSP client
func NewContainerStorageProvider(credentials *storageprovider.Credentials) (*ContainerStorageProvider, error) {
	log.Trace(">>>>> NewContainerStorageProvider")
	defer log.Trace("<<<<< NewContainerStorageProvider")

	// Initialize the container provider client here so we don't have to do it in every method
	client, err := getCspClient(credentials)
	if err != nil {
		log.Errorf("Failed to initialize CSP client, Error: %s", err.Error())
		return nil, err
	}

	csp := &ContainerStorageProvider{
		Credentials: credentials,
		Client:      client,
	}

	log.Trace("Attempting initial login to CSP")
	status, err := csp.login()
	if status != http.StatusOK {
		log.Errorf("Failed to login to CSP.  Status code: %d.  Error: %s", status, err.Error())
		return nil, err
	}
	if err != nil {
		log.Errorf("Failed to login to CSP.  Error: %s", err.Error())
		return nil, err
	}

	return csp, nil
}

// login performs initial login to the CSP as well as periodic login if a session has expired
func (provider *ContainerStorageProvider) login() (int, error) {
	response := &model.Token{}
	var errorResponse *ErrorsPayload

	// Storage-Provider
	token := &model.Token{
		Username: provider.Credentials.Username,
		Password: provider.Credentials.Password,
	}

	// If serviceName is not specified (i.e, Off-Array), then pass the array IP address as well.
	if provider.Credentials.ServiceName != "" {
		log.Infof("About to attempt login to CSP for backend %s", provider.Credentials.Backend)
		token.ArrayIP = provider.Credentials.Backend
	}

	loginMutex.Lock(provider.Credentials.Backend)
	defer loginMutex.Unlock(provider.Credentials.Backend)

	status, err := provider.Client.DoJSON(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/tokens",
			Payload:       token,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return status, handleError(status, errorResponse)
	}
	provider.AuthToken = response.SessionToken

	return status, err
}

// invoke is used to invoke all methods against the CSP. Error handling should be added here.
// Currently, it will login again if the server responds with a status code of unauthorized.
func (provider *ContainerStorageProvider) invoke(request *connectivity.Request) (status int, err error) {
	request.Header = make(map[string]string)
	// Perform login attempt when AuthToken is empty
	if provider.AuthToken == "" {
		log.Info("Cached auth-token is empty, attempting login to CSP")
		status, err = provider.login()
		if status != http.StatusOK {
			log.Errorf("Failed login attempt. Status %d. Error: %s", status, err.Error())
			return status, err
		}
		if err != nil {
			log.Errorf("Error while attempting login to CSP. Error: %s", err.Error())
			return http.StatusInternalServerError, err
		}
		log.Info("Successfully re-generated new login auth-token")
	}

	request.Header[tokenHeader] = provider.AuthToken
	if provider.Credentials.ServiceName != "" {
		log.Tracef("About to invoke CSP request for backend %s", provider.Credentials.Backend)
		request.Header[arrayIPHeader] = provider.Credentials.Backend
	}

	// Temporary copy of the Path as it gets modified/changed in the DoJSON() method.
	// This is required to re-attempt with the original request once login is successful.
	reqPath := request.Path
	status, err = provider.Client.DoJSON(request)
	if status == http.StatusOK {
		return status, nil
	}
	if status == http.StatusUnauthorized {
		log.Info("Received unauthorization error. Attempting login...")
		status, err = provider.login()
		if status != http.StatusOK {
			log.Errorf("Failed login during re-attempt. Status %d. Error: %s", status, err.Error())
			return status, err
		}
		if err != nil {
			log.Errorf("Error while login to CSP during re-attempt. Error: %s", err.Error())
			return http.StatusInternalServerError, err
		}
		request.Path = reqPath      // Set the original path value
		request.ResponseError = nil // Reset the previous error response
		log.Tracef("Re-attempting the request: %+v", request)
		return provider.invoke(request) // Recursive invoke call with new token
	}
	log.Tracef("Replying with status code: %v", status)
	return status, err
}

// SetNodeContext is used to provide host information to the CSP
func (provider *ContainerStorageProvider) SetNodeContext(node *model.Node) error {
	var errorResponse *ErrorsPayload

	// Configure the node information on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/hosts",
			Payload:       &node,
			Response:      nil,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		handleError(status, errorResponse)
	}
	return err
}

// GetNodeContext is used to get host information from the CSP
func (provider *ContainerStorageProvider) GetNodeContext(nodeUUID string) (*model.Node, error) {

	// TODO: Uncomment below section once CSP implementation is ready
	/*
		dataWrapper := &DataWrapper{
			Data: &model.Volume{},
		}
		var errorResponse *ErrorsPayload

		// Get the node information from the array
		status, err := provider.invoke(
			&connectivity.Request{
				Action:        "GET",
				Path:          fmt.Sprintf("/containers/v1/hosts/%s", nodeUUID),
				Payload:       nil,
				Response:      &dataWrapper,
				ResponseError: errorResponse,
			},
		)
		if status == http.StatusNotFound {
			return nil, nil
		}
		if errorResponse != nil {
			return nil, handleError(status, errorResponse)
		}
		return dataWrapper.Data.(*model.Node), err
	*/
	return nil, nil
}

// CreateVolume creates a volume on the CSP
func (provider *ContainerStorageProvider) CreateVolume(name, description string, size int64, opts map[string]interface{}) (*model.Volume, error) {
	log.Tracef(">>>>> CreateVolume, name: %v, size: %v, opts: %v", name, size, opts)
	defer log.Trace("<<<<< CreateVolume")

	response := &model.Volume{}
	var errorResponse *ErrorsPayload

	volume := &model.Volume{
		Name:        name,
		Size:        size,
		Description: description,
		Config:      opts,
	}

	// Create the volume on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/volumes",
			Payload:       &volume,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// CreateSnapshotGroup creates a snapshot group on the CSP
func (provider *ContainerStorageProvider) CreateSnapshotGroup(name, sourceVolumeGroupID string, opts map[string]interface{}) (*model.SnapshotGroup, error) {
	log.Tracef(">>>>> CreateSnapshotGroup, name: %s, sourceVolumeGroupID: %s", name, sourceVolumeGroupID)
	defer log.Trace("<<<<< CreateSnapshotGroup")

	response := &model.SnapshotGroup{}
	var errorResponse *ErrorsPayload

	// TODO: convert opts to any parameters needed for snapshotGroup
	snapshot_group := &model.SnapshotGroup{
		Name:                name,
		SourceVolumeGroupID: sourceVolumeGroupID,
	}

	// Create the snapshot group on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/snapshot_groups",
			Payload:       &snapshot_group,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// DeleteSnapshotGroup deletes a snapshot group on the CSP
func (provider *ContainerStorageProvider) DeleteSnapshotGroup(id string) error {
	log.Tracef(">>>>> DeleteSnapshotGroup, id: %s", id)
	defer log.Trace("<<<<< DeleteSnapshotGroup")

	var errorResponse *ErrorsPayload

	// Delete the snapshot group on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "DELETE",
			Path:          fmt.Sprintf("/containers/v1/snapshot_groups/%s", id),
			Payload:       nil,
			Response:      nil,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return handleError(status, errorResponse)
	}
	if err != nil {
		return err
	}

	return nil
}

// CreateVolumeGroup creates a volume group on the CSP
func (provider *ContainerStorageProvider) CreateVolumeGroup(name, description string, opts map[string]interface{}) (*model.VolumeGroup, error) {
	log.Tracef(">>>>> CreateVolumeGroup, name: %s, opts: %+v", name, opts)
	defer log.Trace("<<<<< CreateVolumeGroup")

	response := &model.VolumeGroup{}
	var errorResponse *ErrorsPayload

	volume_group := &model.VolumeGroup{
		Name:        name,
		Description: description,
		Config:      opts,
	}

	// Create the volume group on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/volume_groups",
			Payload:       &volume_group,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// DeleteVolumeGroup deletes a volume group on the CSP
func (provider *ContainerStorageProvider) DeleteVolumeGroup(id string) error {
	log.Tracef(">>>>> DeleteVolumeGroup, id: %s", id)
	defer log.Trace("<<<<< DeleteVolumeGroup")

	var errorResponse *ErrorsPayload

	// Delete the volume group on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "DELETE",
			Path:          fmt.Sprintf("/containers/v1/volume_groups/%s", id),
			Payload:       nil,
			Response:      nil,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return handleError(status, errorResponse)
	}
	if err != nil {
		return err
	}

	return nil
}

// CloneVolume clones a volume on the CSP
// nolint : gocyclo
func (provider *ContainerStorageProvider) CloneVolume(name, description, sourceID, snapshotID string, size int64, opts map[string]interface{}) (*model.Volume, error) {
	log.Tracef(">>>>> CloneVolume with name: %v, description: %v, sourceID: %v, snapshotID %v, size: %d", name, description, sourceID, snapshotID, size)
	defer log.Trace("<<<<< CloneVolume")

	// Check for empty name
	if name == "" {
		return nil, fmt.Errorf("Clone name cannot be empty")
	}
	// Check if both source and snapshot values are empty
	if sourceID == "" && snapshotID == "" {
		return nil, fmt.Errorf("One of sourceID or snapshotID must be specified")
	}

	var snapshot *model.Snapshot
	var err error
	if sourceID == "" {
		log.Tracef("Request to create clone from snapshot ID %s", snapshotID)

		snapshot, err = provider.GetSnapshot(snapshotID)
		if err != nil {
			log.Errorf("Failed to get snapshot with id %s for clone.  Error: %s", snapshotID, err.Error())
			return nil, err
		}
		if snapshot == nil {
			log.Errorf("Snapshot with ID %s not found", snapshotID)
			return nil, errors.New("Could not find snapshot with id " + snapshotID)
		}
	} else if snapshotID == "" {
		log.Tracef("Creating snapshot for new clone from source volume %s", sourceID)

		snapshotName := snapshotPrefix + time.Now().Format(time.RFC3339)
		snapshot, err = provider.CreateSnapshot(snapshotName, snapshotName, sourceID, nil)
		if err != nil {
			log.Errorf("Failed to create snapshot for clone.  Error: %s", err.Error())
			return nil, err
		}
		if snapshot == nil {
			log.Error("Failed to create new snapshot for volume clone")
			return nil, errors.New("Could not create new snapshot for volume clone")
		}
	}

	var volume *model.Volume
	if size != 0 {
		// Check for valid resize value. Must be equal or higher than the snapshot size.
		if size < snapshot.Size {
			errMsg := fmt.Sprintf("Clone size %v requested is smaller than the snapshot size %v", size, snapshot.Size)
			return nil, fmt.Errorf("Could not create new volume clone, err: %s", errMsg)
		}
		// Create clone with new size (>= parent volume size)
		volume = &model.Volume{
			Name:        name,
			Description: description,
			Size:        size,
			BaseSnapID:  snapshot.ID,
			Clone:       true,
			Config:      opts,
		}
	} else {
		// Don't set the clone size input (Defaults to parent volume size)
		volume = &model.Volume{
			Name:        name,
			Description: description,
			BaseSnapID:  snapshot.ID,
			Clone:       true,
			Config:      opts,
		}
	}
	log.Tracef("Clone requested with volume config: %+v", volume)

	response := &model.Volume{}
	var errorResponse *ErrorsPayload

	// Clone the volume on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/volumes",
			Payload:       &volume,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		// Delete the snapshot that was created above
		if snapshotID == "" && snapshot != nil {
			log.Tracef("Deleting snapshot %v", snapshot.Name)
			provider.DeleteSnapshot(snapshot.ID)
		}

		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// DeleteVolume will remove a volume from the CSP
// nolint: dupl
func (provider *ContainerStorageProvider) DeleteVolume(id string, force bool) error {
	log.Tracef(">>>>> DeleteVolume, id: %v, force: %v", id, force)
	defer log.Trace("<<<<< DeleteVolume")

	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "DELETE",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s?force=%v", id, force),
			Payload:       nil,
			Response:      nil,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		log.Errorf("Failed to delete volume with id %s", id)
		return handleError(status, errorResponse)
	}

	return err
}

// PublishVolume will make a volume visible (add an ACL) to the given host
func (provider *ContainerStorageProvider) PublishVolume(id, hostUUID, accessProtocol string) (*model.PublishInfo, error) {
	response := &model.PublishInfo{}
	var errorResponse *ErrorsPayload

	publishOptions := &model.PublishOptions{
		HostUUID:       hostUUID,
		AccessProtocol: accessProtocol,
	}

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "PUT",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s/actions/publish", id),
			Payload:       &publishOptions,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// UnpublishVolume will make a volume invisible (remove an ACL) from the given host
func (provider *ContainerStorageProvider) UnpublishVolume(id, hostUUID string) error {
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "PUT",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s/actions/unpublish", id),
			Payload:       &model.PublishOptions{HostUUID: hostUUID},
			Response:      nil,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return handleError(status, errorResponse)
	}

	return err
}

// ExpandVolume will expand the volume to reqeusted size
func (provider *ContainerStorageProvider) ExpandVolume(id string, requestBytes int64) (*model.Volume, error) {
	log.Tracef(">>>>> ExpandVolume, id: %s, requestBytes: %d", id, requestBytes)
	defer log.Traceln("<<<<< ExpandVolume")

	response := &model.Volume{}
	var errorResponse *ErrorsPayload

	volume := &model.Volume{
		ID:   id,
		Size: requestBytes,
	}

	// Expand volume on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "PUT",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s", id),
			Payload:       &volume,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// EditVolume edits a volume on the CSP
func (provider *ContainerStorageProvider) EditVolume(id string, opts map[string]interface{}) (*model.Volume, error) {
	log.Tracef(">>>>> EditVolume, id: %s, opts: %v", id, opts)
	defer log.Trace("<<<<< EditVolume")

	response := &model.Volume{}
	var errorResponse *ErrorsPayload

	volume := &model.Volume{
		ID: id,
	}

	// description is part of volume object and should be removed from opts
	if desc, ok := opts[descriptionKey]; ok {
		delete(opts, descriptionKey)
		volume.Description = desc.(string)
	}

	// volumeGroupId is part of volume object and should be removed from opts
	if val, ok := opts[volumeGroupIdKey]; ok {
		delete(opts, volumeGroupIdKey)
		volume.VolumeGroupId = val.(string)
	}
	volume.Config = opts

	// Edit the volume on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "PUT",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s", id),
			Payload:       &volume,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// GetVolume will return information about the given volume
func (provider *ContainerStorageProvider) GetVolume(id string) (*model.Volume, error) {
	response := &model.Volume{}
	var errorResponse *ErrorsPayload
	var status int
	var err error
	status, err = provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s", id),
			Payload:       nil,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)

	if status == http.StatusOK {
		return response, nil
	}

	if status == http.StatusNotFound {
		return nil, nil
	}

	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return nil, err
}

// GetVolumeByName will return information about the given volume
func (provider *ContainerStorageProvider) GetVolumeByName(name string) (*model.Volume, error) {
	response := make([]*model.Volume, 0)
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/volumes?name=%s", name),
			Payload:       nil,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if status == http.StatusNotFound {
		return nil, nil
	}

	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	// there should only be one volume
	values := reflect.ValueOf(response)
	if values.Len() == 0 {
		log.Errorf("Could not find volume %s in json response", name)
		return nil, fmt.Errorf("Could not find volume named %s in json response", name)
	}
	log.Tracef("Found %d volumes", values.Len())

	volume := &model.Volume{}
	err = jsonutil.Decode(values.Index(0).Interface(), volume)
	if err != nil {
		return nil, fmt.Errorf("Error while decoding the volume response, err: %s", err.Error())
	}

	return volume, err
}

// GetVolumes returns all of the volumes from the CSP
func (provider *ContainerStorageProvider) GetVolumes() ([]*model.Volume, error) {
	response := make([]*model.Volume, 0)
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          "/containers/v1/volumes",
			Payload:       nil,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	// unmarshal each volume... TODO: do this via reflection for all object types
	values := reflect.ValueOf(response)
	log.Tracef("Found %d volumes", values.Len())
	volumes := make([]*model.Volume, values.Len())
	for i := 0; i < values.Len(); i++ {
		volume := &model.Volume{}
		err = jsonutil.Decode(values.Index(i).Interface(), volume)
		if err != nil {
			return nil, fmt.Errorf("Error while decoding the volume response, err: %s", err.Error())
		}
		volumes[i] = volume
	}

	return volumes, err
}

// GetSnapshots returns all of the snapshots for the given source volume from the CSP
func (provider *ContainerStorageProvider) GetSnapshots(volumeID string) ([]*model.Snapshot, error) {
	response := make([]*model.Snapshot, 0)
	var errorResponse *ErrorsPayload

	var path string
	if volumeID == "" {
		path = fmt.Sprintf("/containers/v1/snapshots")
	} else {
		path = fmt.Sprintf("/containers/v1/snapshots?volume_id=%s", volumeID)
	}

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          path,
			Payload:       nil,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	// unmarshal each snapshot... TODO: do this via reflection for all object types
	values := reflect.ValueOf(response)
	log.Tracef("Found %d snapshots", values.Len())
	snapshots := make([]*model.Snapshot, values.Len())
	for i := 0; i < values.Len(); i++ {
		snapshot := &model.Snapshot{}
		err = jsonutil.Decode(values.Index(i).Interface(), snapshot)
		if err != nil {
			return nil, fmt.Errorf("Error while decoding the snapshot response, err: %s", err.Error())
		}
		snapshots[i] = snapshot
	}
	return snapshots, err
}

// GetSnapshot will return information about the given snapshot
func (provider *ContainerStorageProvider) GetSnapshot(id string) (*model.Snapshot, error) {
	log.Trace(">>>>> GetSnapshot, id:", id)
	defer log.Trace("<<<<< GetSnapshot")

	response := &model.Snapshot{}
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/snapshots/%s", id),
			Payload:       nil,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)
	if status == http.StatusNotFound {
		return nil, nil
	}

	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return response, err
}

// GetSnapshotByName will return information about the given snapshot
func (provider *ContainerStorageProvider) GetSnapshotByName(name string, volumeID string) (*model.Snapshot, error) {

	// Get all snapshots for the given snapshot name and source volume ID
	response := make([]*model.Snapshot, 0)
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/snapshots?name=%s&volume_id=%s", name, volumeID),
			Payload:       nil,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)

	if status == http.StatusNotFound {
		return nil, nil
	}

	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	if err != nil {
		return nil, err
	}

	// There should only be one snapshot
	values := reflect.ValueOf(response)
	if values.Len() == 0 {
		log.Errorf("Could not find snapshot %s in json response", name)
		return nil, fmt.Errorf("Could not find snapshot named %s in json response", name)
	}
	log.Tracef("Found %d snapshots", values.Len())

	snapshot := &model.Snapshot{}
	err = jsonutil.Decode(values.Index(0).Interface(), snapshot)
	if err != nil {
		return nil, fmt.Errorf("Error while decoding the snapshot response, err: %s", err.Error())
	}
	return snapshot, nil
}

// CreateSnapshot will create a new snapshot of the given name of the source volume ID
func (provider *ContainerStorageProvider) CreateSnapshot(name, description, sourceVolumeID string, opts map[string]interface{}) (*model.Snapshot, error) {
	log.Tracef(">>>>> CreateSnapshot, name: %v, description: %v, sourceVolumeID: %v", name, description, sourceVolumeID)
	defer log.Traceln("<<<<< CreateSnapshot")

	response := &model.Snapshot{}
	var errorResponse *ErrorsPayload

	snapshot := &model.Snapshot{
		Name:        name,
		VolumeID:    sourceVolumeID,
		Description: description,
		Config:      opts,
	}

	// Create the snapshot on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/snapshots",
			Payload:       &snapshot,
			Response:      &response,
			ResponseError: &errorResponse,
		},
	)

	if errorResponse != nil {
		err := handleError(status, errorResponse)
		// TODO: When snapshot creation fails due to insufficient space,
		//       then CSP should return appropriate error message containing string "insufficient storage space"
		if strings.Contains(err.Error(), "insufficient storage space") {
			return nil, grpcstatus.Error(codes.ResourceExhausted,
				"There is not enough space on the storage system to handle the create snapshot request")
		}
		return nil, err
	}

	return response, err
}

// DeleteSnapshot will remove a snapshot from the CSP
// nolint: dupl
func (provider *ContainerStorageProvider) DeleteSnapshot(id string) error {
	log.Trace(">>>>> DeleteSnapshot, id:", id)
	defer log.Trace("<<<<< DeleteSnapshot")

	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "DELETE",
			Path:          fmt.Sprintf("/containers/v1/snapshots/%s", id),
			Payload:       nil,
			Response:      nil,
			ResponseError: &errorResponse,
		},
	)
	if errorResponse != nil {
		log.Errorf("Failed to delete snapshot with id %s", id)
		return handleError(status, errorResponse)
	}

	return err
}

// get CSP client
func getCspClient(credentials *storageprovider.Credentials) (*connectivity.Client, error) {

	// On-Array CSP
	if credentials.ServiceName == "" {
		if credentials.ContextPath == "" {
			credentials.ContextPath = storageprovider.DefaultContextPath
		}
		if credentials.ServicePort == 0 {
			credentials.ServicePort = storageprovider.DefaultServicePort
		}
		cspURI := fmt.Sprintf("https://%s:%d%s", credentials.Backend, credentials.ServicePort, credentials.ContextPath)

		log.Tracef(">>>>> getCspClient (direct-connect) using URI %s and username %s", cspURI, credentials.Username)
		defer log.Trace("<<<<< getCspClient")

		// Setup HTTPS client
		cspClient := connectivity.NewHTTPSClientWithTimeout(
			cspURI,
			&http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			CspClientTimeout,
		)

		return cspClient, nil
	}

	// Off-Array CSP
	cspURI := fmt.Sprintf("http://%s:%d", credentials.ServiceName, credentials.ServicePort)

	log.Tracef(">>>>> getCspClient (service) using URI %s and username %s", cspURI, credentials.Username)
	defer log.Trace("<<<<< getCspClient")

	// Setup HTTP client to the CSP service
	cspClient := connectivity.NewHTTPClient(cspURI)

	return cspClient, nil
}

func handleError(httpStatus int, errorResponse *ErrorsPayload) error {
	var errorString strings.Builder
	for _, element := range errorResponse.Errors {
		errorString.WriteString(fmt.Sprintf("Error code (%s) and message (%s)", element.Code, element.Message))
	}
	log.Errorf("HTTP error %d.  Error payload: %s", httpStatus, errorString.String())
	return fmt.Errorf("Request failed with status code %d and errors %s", httpStatus, errorString.String())
}
