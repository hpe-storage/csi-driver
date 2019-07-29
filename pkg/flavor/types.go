// Copyright 2019 Hewlett Packard Enterprise Development LP

package flavor

import "github.com/hpe-storage/common-host-libs/model"

const (
	// Vanilla flavored CSI driver
	Vanilla = "vanilla"

	// Kubernetes flavored CSI driver
	Kubernetes = "kubernetes"
)

// Flavor defines the interface for the type of driver
type Flavor interface {
	ConfigureAnnotations(claimName string, parameters map[string]string) (map[string]string, error)

	LoadNodeInfo(*model.Node) (string, error)
	UnloadNodeInfo()
	GetNodeInfo(nodeID string) (*model.Node, error)
}
