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

	"github.com/hpe-storage/common-host-libs/connectivity"
	"github.com/hpe-storage/common-host-libs/jsonutil"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/storageprovider"
)

const (
	snapshotPrefix = "snap-for-clone-"
)

// DataWrapper is used to represent a generic JSON API payload
type DataWrapper struct {
	Data interface{} `json:"data"`
}

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
		log.Trace("Failed to initialize CSP client")
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
		log.Tracef("Failed to login to CSP.  Error: %s", err.Error())
		return nil, err
	}

	return csp, nil
}

// login performs initial login to the CSP as well as periodic login if a session has expired
func (provider *ContainerStorageProvider) login() (int, error) {
	dataWrapper := &DataWrapper{
		Data: &model.Token{},
	}
	var errorResponse *ErrorsPayload

	// Storage-Provider
	token := &model.Token{
		Username: provider.Credentials.Username,
		Password: provider.Credentials.Password,
	}

	// If serviceName is not specified (i.e, Off-Array), then pass the array IP address as well.
	if provider.Credentials.ServiceName != "" {
		token.ArrayIP = provider.Credentials.Backend
	}

	status, err := provider.Client.DoJSON(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/tokens",
			Payload:       &DataWrapper{Data: token},
			Response:      &dataWrapper,
			ResponseError: errorResponse,
		},
	)
	if errorResponse != nil {
		return status, handleError(status, errorResponse)
	}
	provider.AuthToken = dataWrapper.Data.(*model.Token).SessionToken

	return status, err
}

// invoke is used to invoke all methods against the CSP. Error handling should be added here.
// Currently, it will login again if the server responds with a status code of unauthorized.
func (provider *ContainerStorageProvider) invoke(request *connectivity.Request) (int, error) {
	request.Header = make(map[string]string)
	request.Header["x-auth-token"] = provider.AuthToken
	if provider.Credentials.ServiceName != "" {
		request.Header["x-array-ip"] = provider.Credentials.Backend
	}

	// Temporary copy of the Path as it gets modified/changed in the DoJSON() method.
	// This is required to re-attempt with the original request once login is successful.
	reqPath := request.Path
	status, err := provider.Client.DoJSON(request)
	if status == http.StatusUnauthorized {
		log.Info("Received unauthorization error. Attempting login...")
		status, err = provider.login()
		if status != http.StatusOK {
			log.Errorf("Failed login during re-attempt. Status %d. Error: %s", status, err.Error())
			return status, err
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
			Path:          "/containers/v1/nodes",
			Payload:       &DataWrapper{Data: node},
			Response:      nil,
			ResponseError: errorResponse,
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
				Path:          fmt.Sprintf("/containers/v1/nodes/%s", nodeUUID),
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
func (provider *ContainerStorageProvider) CreateVolume(name string, size int64, opts map[string]interface{}) (*model.Volume, error) {
	log.Tracef(">>>>> CreateVolume, name: %v, size: %v, opts: %v", name, size, opts)
	defer log.Trace("<<<<< CreateVolume")

	dataWrapper := &DataWrapper{
		Data: &model.Volume{},
	}
	var errorResponse *ErrorsPayload

	volume := &model.Volume{
		Name:   name,
		Size:   size,
		Config: opts,
	}

	// Create the volume on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/volumes",
			Payload:       &DataWrapper{Data: volume},
			Response:      &dataWrapper,
			ResponseError: errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return dataWrapper.Data.(*model.Volume), err
}

// CloneVolume clones a volume on the CSP
// nolint : gocyclo
func (provider *ContainerStorageProvider) CloneVolume(name, sourceID, snapshotID string, opts map[string]interface{}) (*model.Volume, error) {
	log.Tracef(">>>>> CloneVolume with name: %v, sourceID: %v, snapshotID %v", name, sourceID, snapshotID)
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
		log.Trace("Creating snapshot for new clone")

		snapshotName := snapshotPrefix + time.Now().Format(time.RFC3339)
		snapshot, err = provider.CreateSnapshot(snapshotName, sourceID)
		if err != nil {
			log.Errorf("Failed to create snapshot for clone.  Error: %s", err.Error())
			return nil, err
		}
		if snapshot == nil {
			log.Error("Failed to create new snapshot for volume clone")
			return nil, errors.New("Could not create new snapshot for volume clone")
		}
	}

	dataWrapper := &DataWrapper{
		Data: &model.Volume{},
	}
	var errorResponse *ErrorsPayload

	volume := &model.Volume{
		Name:       name,
		BaseSnapID: snapshot.ID,
		Clone:      true,
		Config:     opts,
	}

	// Clone the volume on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/volumes",
			Payload:       &DataWrapper{Data: volume},
			Response:      &dataWrapper,
			ResponseError: errorResponse,
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

	return dataWrapper.Data.(*model.Volume), err
}

// DeleteVolume will remove a volume from the CSP
// nolint: dupl
func (provider *ContainerStorageProvider) DeleteVolume(id string) error {
	log.Trace(">>>>> DeleteVolume, id:", id)
	defer log.Trace("<<<<< DeleteVolume")

	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "DELETE",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s", id),
			Payload:       nil,
			Response:      nil,
			ResponseError: errorResponse,
		},
	)
	if errorResponse != nil {
		log.Errorf("Failed to delete volume with id %s", id)
		return handleError(status, errorResponse)
	}

	return err
}

// PublishVolume will make a volume visible (add an ACL) to the given node
func (provider *ContainerStorageProvider) PublishVolume(id, nodeID string) (*model.PublishInfo, error) {
	dataResponse := &DataWrapper{
		Data: &model.PublishInfo{},
	}

	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s/actions/publish", id),
			Payload:       &DataWrapper{Data: &model.PublishOptions{NodeID: nodeID}},
			Response:      &dataResponse,
			ResponseError: errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return dataResponse.Data.(*model.PublishInfo), err
}

// UnpublishVolume will make a volume invisible (remove an ACL) from the given node
func (provider *ContainerStorageProvider) UnpublishVolume(id, nodeID string) error {
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s/actions/unpublish", id),
			Payload:       &DataWrapper{Data: &model.PublishOptions{NodeID: nodeID}},
			Response:      nil,
			ResponseError: errorResponse,
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

	dataWrapper := &DataWrapper{
		Data: &model.Volume{},
	}
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
			Payload:       &DataWrapper{Data: volume},
			Response:      &dataWrapper,
			ResponseError: errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	return dataWrapper.Data.(*model.Volume), err
}

// GetVolume will return information about the given volume
func (provider *ContainerStorageProvider) GetVolume(id string) (*model.Volume, error) {
	dataWrapper := &DataWrapper{
		Data: &model.Volume{},
	}
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/volumes/%s", id),
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

	return dataWrapper.Data.(*model.Volume), err
}

// GetVolumeByName will return information about the given volume
func (provider *ContainerStorageProvider) GetVolumeByName(name string) (*model.Volume, error) {
	dataWrapper := &DataWrapper{
		Data: make([]*model.Volume, 0),
	}
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/volumes/detail?name=%s", name),
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

	// there should only be one volume
	values := reflect.ValueOf(dataWrapper.Data)
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
	dataWrapper := &DataWrapper{
		Data: make([]*model.Volume, 0),
	}
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          "/containers/v1/volumes/detail",
			Payload:       nil,
			Response:      &dataWrapper,
			ResponseError: errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	// unmarshal each volume... TODO: do this via reflection for all object types
	values := reflect.ValueOf(dataWrapper.Data)
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
	dataWrapper := &DataWrapper{
		Data: make([]*model.Snapshot, 0),
	}
	var errorResponse *ErrorsPayload

	var path string
	if volumeID == "" {
		path = fmt.Sprintf("/containers/v1/snapshots/detail")
	} else {
		path = fmt.Sprintf("/containers/v1/snapshots/detail?volume_id=%s", volumeID)
	}

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          path,
			Payload:       nil,
			Response:      &dataWrapper,
			ResponseError: errorResponse,
		},
	)
	if errorResponse != nil {
		return nil, handleError(status, errorResponse)
	}

	// unmarshal each snapshot... TODO: do this via reflection for all object types
	values := reflect.ValueOf(dataWrapper.Data)
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

	dataWrapper := &DataWrapper{
		Data: &model.Snapshot{},
	}
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/snapshots/%s", id),
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

	return dataWrapper.Data.(*model.Snapshot), err
}

// GetSnapshotByName will return information about the given snapshot
func (provider *ContainerStorageProvider) GetSnapshotByName(name string, volumeID string) (*model.Snapshot, error) {

	// Get all snapshots for the given snapshot name and source volume ID
	dataWrapper := &DataWrapper{
		Data: make([]*model.Snapshot, 0),
	}
	var errorResponse *ErrorsPayload

	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "GET",
			Path:          fmt.Sprintf("/containers/v1/snapshots/detail?name=%s&volume_id=%s", name, volumeID),
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

	if err != nil {
		return nil, err
	}

	// There should only be one snapshot
	values := reflect.ValueOf(dataWrapper.Data)
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
func (provider *ContainerStorageProvider) CreateSnapshot(name, sourceVolumeID string) (*model.Snapshot, error) {
	log.Tracef(">>>>> CreateSnapshot, name: %v, sourceVolumeID: %v", name, sourceVolumeID)
	defer log.Traceln("<<<<< CreateSnapshot")

	dataWrapper := &DataWrapper{
		Data: &model.Snapshot{},
	}
	var errorResponse *ErrorsPayload

	snapshot := &model.Snapshot{
		Name:     name,
		VolumeID: sourceVolumeID,
	}

	// Create the snapshot on the array
	status, err := provider.invoke(
		&connectivity.Request{
			Action:        "POST",
			Path:          "/containers/v1/snapshots",
			Payload:       &DataWrapper{Data: snapshot},
			Response:      &dataWrapper,
			ResponseError: errorResponse,
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

	return dataWrapper.Data.(*model.Snapshot), err
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
			ResponseError: errorResponse,
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
