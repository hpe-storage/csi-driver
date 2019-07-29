// Copyright 2019 Hewlett Packard Enterprise Development LP

package storageprovider

import (
	"fmt"
	"strconv"

	"github.com/hpe-storage/common-host-libs/model"
)

const (
	arrayIPKey     = "arrayIp"
	usernameKey    = "username"
	passwordKey    = "password"
	portKey        = "port"
	contextPathKey = "path"
	serviceNameKey = "serviceName"
)

// StorageProvider defines the interface to any storage related operations required by CSI and hopefully docker
type StorageProvider interface {
	SetNodeContext(*model.Node) error
	GetNodeContext(nodeID string) (*model.Node, error)
	GetVolume(id string) (*model.Volume, error)
	GetVolumeByName(name string) (*model.Volume, error)
	GetVolumes() ([]*model.Volume, error)
	CreateVolume(name string, size int64, opts map[string]interface{}) (*model.Volume, error)
	CloneVolume(name, sourceID, snapshotID string, opts map[string]interface{}) (*model.Volume, error)
	DeleteVolume(id string) error
	PublishVolume(id, nodeID string) (*model.PublishInfo, error) // Idempotent
	UnpublishVolume(id, nodeID string) error                     // Idempotent
	ExpandVolume(id string, requestBytes int64) (*model.Volume, error)
	GetSnapshot(id string) (*model.Snapshot, error)
	GetSnapshotByName(name string, sourceVolID string) (*model.Snapshot, error)
	GetSnapshots(sourceVolID string) ([]*model.Snapshot, error)
	CreateSnapshot(name, sourceVolID string) (*model.Snapshot, error)
	DeleteSnapshot(id string) error
}

// Credentials defines how a StorageProvider is accessed
type Credentials struct {
	Username    string
	Password    string
	ArrayIP     string
	Port        int
	ContextPath string
	ServiceName string
}

// CreateCredentials creates the credentail object from the given secrets
func CreateCredentials(secrets map[string]string) (*Credentials, error) {
	credentials := &Credentials{}
	// Note: CSI does not perform b64 decoding for secrets as the external-attacher/doryd does it for you.

	// Read the secrets map and populate the Credential object accordingly
	for key, value := range secrets {
		if key == arrayIPKey {
			credentials.ArrayIP = value
		}

		if key == usernameKey {
			credentials.Username = value
		}

		if key == passwordKey {
			credentials.Password = value
		}

		if key == portKey {
			port, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			credentials.Port = port
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
	if credentials.ArrayIP == "" {
		return nil, fmt.Errorf("Missing array IP in the secrets")
	}
	if credentials.ServiceName == "" && credentials.ContextPath == "" {
		return nil, fmt.Errorf("Both service name and context path are missing in the secrets")
	}
	if credentials.Port == 0 {
		return nil, fmt.Errorf("Missing port in the secrets")
	}

	return credentials, nil
}
