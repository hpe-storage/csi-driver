// Copyright 2019 Hewlett Packard Enterprise Development LP

package storageprovider

import (
	"fmt"
	"strconv"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
)

const (
	backendKey     = "backend"
	usernameKey    = "username"
	passwordKey    = "password"
	servicePortKey = "servicePort"
	contextPathKey = "path"
	serviceNameKey = "serviceName"
	// DefaultContextPath if not set for on-array csp
	DefaultContextPath = "/csp"
	// DefaultServicePort if not set for on-array csp
	DefaultServicePort = 443
)

// StorageProvider defines the interface to any storage related operations required by CSI and hopefully docker
type StorageProvider interface {
	SetNodeContext(*model.Node) error
	GetNodeContext(nodeID string) (*model.Node, error)
	GetVolume(id string) (*model.Volume, error)
	GetVolumeByName(name string) (*model.Volume, error)
	GetVolumes() ([]*model.Volume, error)
	CreateVolume(name, description string, size int64, opts map[string]interface{}) (*model.Volume, error)
	CloneVolume(name, description, sourceID, snapshotID string, size int64, opts map[string]interface{}) (*model.Volume, error)
	DeleteVolume(id string, force bool) error
	PublishVolume(id, hostUUID, accessProtocol string) (*model.PublishInfo, error) // Idempotent
	UnpublishVolume(id, hostUUID string) error                                     // Idempotent
	ExpandVolume(id string, requestBytes int64) (*model.Volume, error)
	GetSnapshot(id string) (*model.Snapshot, error)
	GetSnapshotByName(name string, sourceVolID string) (*model.Snapshot, error)
	GetSnapshots(sourceVolID string) ([]*model.Snapshot, error)
	CreateSnapshot(name, description, sourceVolID string, opts map[string]interface{}) (*model.Snapshot, error)
	DeleteSnapshot(id string) error
	EditVolume(id string, opts map[string]interface{}) (*model.Volume, error)
	CreateVolumeGroup(name, description string, opts map[string]interface{}) (*model.VolumeGroup, error)
	DeleteVolumeGroup(id string) error
	CreateSnapshotGroup(name, sourceVolumeGroupID string, opts map[string]interface{}) (*model.SnapshotGroup, error)
	DeleteSnapshotGroup(id string) error
}

// Credentials defines how a StorageProvider is accessed
type Credentials struct {
	Username    string
	Password    string
	Backend     string
	ServicePort int
	ContextPath string
	ServiceName string
}

// CreateCredentials creates the credentail object from the given secrets
func CreateCredentials(secrets map[string]string) (*Credentials, error) {
	log.Trace(">>>>> CreateCredentials")
	defer log.Trace("<<<<< CreateCredentials")

	// When secrets specified
	if secrets == nil || len(secrets) == 0 {
		return nil, fmt.Errorf("No secrets have been provided")
	}

	credentials := &Credentials{}
	// Note: CSI does not perform b64 decoding for secrets as the external-attacher/doryd does it for you.

	// Read the secrets map and populate the Credential object accordingly
	for key, value := range secrets {
		if key == backendKey {
			credentials.Backend = value
		}

		if key == usernameKey {
			credentials.Username = value
		}

		if key == passwordKey {
			credentials.Password = value
		}

		if key == servicePortKey {
			port, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			credentials.ServicePort = port
		}

		if key == serviceNameKey {
			credentials.ServiceName = value
		}

		if key == contextPathKey {
			credentials.ContextPath = value
		}
	}

	// Check for mandatory secret parameters
	if credentials.Username == "" {
		return nil, fmt.Errorf("Missing username in the secrets")
	}
	if credentials.Password == "" {
		return nil, fmt.Errorf("Missing password in the secrets")
	}
	if credentials.Backend == "" {
		return nil, fmt.Errorf("Missing backend IP in the secrets")
	}
	if credentials.ServiceName != "" && credentials.ServicePort == 0 {
		return nil, fmt.Errorf("Missing port in the secrets")
	}

	return credentials, nil
}
