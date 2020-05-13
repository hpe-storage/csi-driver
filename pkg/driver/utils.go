// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
)

// ParseEndpoint parses the gRPC endpoint provided
func ParseEndpoint(ep string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(ep), "unix://") || strings.HasPrefix(strings.ToLower(ep), "tcp://") {
		s := strings.SplitN(ep, "://", 2)
		if s[1] != "" {
			return s[0], s[1], nil
		}
	}
	return "", "", fmt.Errorf("Invalid endpoint: %v", ep)
}

// NewControllerServiceCapability wraps the given type into a proper capability as expected by the spec
func NewControllerServiceCapability(cap csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
	return &csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: cap,
			},
		},
	}
}

// NewNodeServiceCapability wraps the given type into a property capability as expected by the spec
func NewNodeServiceCapability(cap csi.NodeServiceCapability_RPC_Type) *csi.NodeServiceCapability {
	return &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: cap,
			},
		},
	}
}

// NewPluginCapabilityVolumeExpansion wraps the given volume expansion into a plugin capability volume expansion required by the spec
func NewPluginCapabilityVolumeExpansion(cap csi.PluginCapability_VolumeExpansion_Type) *csi.PluginCapability_VolumeExpansion {
	return &csi.PluginCapability_VolumeExpansion{Type: cap}
}

// NewVolumeCapabilityAccessMode wraps the given access mode into a volume capability access mode required by the spec
func NewVolumeCapabilityAccessMode(mode csi.VolumeCapability_AccessMode_Mode) *csi.VolumeCapability_AccessMode {
	return &csi.VolumeCapability_AccessMode{Mode: mode}
}

func logGRPC(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	log.Infof("GRPC call: %s", info.FullMethod)
	log.Infof("GRPC request: %+v", protosanitizer.StripSecrets(req))
	resp, err := handler(ctx, req)
	if err != nil {
		log.Errorf("GRPC error: %v", err)
	} else {
		log.Infof("GRPC response: %+v", protosanitizer.StripSecrets(resp))
	}
	return resp, err
}

// Helper function to convert the epoch seconds to timestamp object
func convertSecsToTimestamp(seconds int64) *timestamp.Timestamp {
	return &timestamp.Timestamp{
		Seconds: seconds,
	}
}

// writeData persists data as json file at the provided location. Creates new directory if not already present.
func writeData(dir string, fileName string, data interface{}) error {
	dataFilePath := filepath.Join(dir, fileName)
	log.Tracef("saving data file [%s]", dataFilePath)

	// Encode from json object
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Attempt create of staging dir, as CSI attacher can remove the directory
	// while operation is still pending(during retries)
	if err = os.MkdirAll(dir, 0750); err != nil {
		log.Errorf("Failed to create dir %s, %v", dir, err.Error())
		return err
	}

	// Write to file
	err = ioutil.WriteFile(dataFilePath, jsonData, 0600)
	if err != nil {
		log.Errorf("Failed to write to file [%s], %v", dataFilePath, err.Error())
		return err
	}
	log.Tracef("Data file [%s] saved successfully", dataFilePath)
	return nil
}

func removeDataFile(dirPath string, fileName string) error {
	log.Tracef(">>>>> removeDataFile, dir: %s, fileName: %s", dirPath, fileName)
	defer log.Trace("<<<<< removeDataFile")
	filePath := path.Join(dirPath, fileName)
	return util.FileDelete(filePath)
}
