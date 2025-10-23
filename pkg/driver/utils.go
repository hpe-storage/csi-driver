// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
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
func NewControllerServiceCapability(capability csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
	return &csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: capability,
			},
		},
	}
}

// NewNodeServiceCapability wraps the given type into a property capability as expected by the spec
func NewNodeServiceCapability(capability csi.NodeServiceCapability_RPC_Type) *csi.NodeServiceCapability {
	return &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: capability,
			},
		},
	}
}

// NewPluginCapabilityVolumeExpansion wraps the given volume expansion into a plugin capability volume expansion required by the spec
func NewPluginCapabilityVolumeExpansion(capability csi.PluginCapability_VolumeExpansion_Type) *csi.PluginCapability_VolumeExpansion {
	return &csi.PluginCapability_VolumeExpansion{Type: capability}
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

// isValidIP checks whether the provided string is a valid IP address.
// It returns true if the input is non-empty and can be parsed as an IP address,
// otherwise it returns false.
func isValidIP(ip string) bool {
	return ip != "" && net.ParseIP(ip) != nil
}

// GetNvmeInitiator returns the NVMe host NQN as a string
func GetNvmeInitiator() (string, error) {
	data, err := ioutil.ReadFile("/etc/nvme/hostnqn")
	if err != nil {
		return "", err
	}
	nqn := strings.TrimSpace(string(data))
	return nqn, nil
}

func getNetworkInterfaceIP(targetIP string, interfaceCIDRs []*string) (string, error) {
	log.Tracef(">>>>> GetNetworkInterfaceIP with targetIP: %s, interfaceCIDRs: %v", targetIP, interfaceCIDRs)
	defer log.Trace("<<<<< GetNetworkInterfaceIP")

	if targetIP == "" {
		return "", fmt.Errorf("target IP address cannot be empty")
	}

	if len(interfaceCIDRs) == 0 {
		return "", fmt.Errorf("interface CIDR list cannot be empty")
	}

	ip := net.ParseIP(targetIP)
	if ip == nil {
		return "", fmt.Errorf("invalid target IP address format: %s", targetIP)
	}

	var parseErrors []string
	for _, cidr := range interfaceCIDRs {
		if cidr == nil || *cidr == "" {
			continue // Skip empty CIDR entries
		}

		// Parse the CIDR to get both the interface IP and the network
		interfaceIP, ipnet, err := net.ParseCIDR(*cidr)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("invalid CIDR format '%s': %v", *cidr, err))
			continue
		}

		if ipnet.Contains(ip) {
			// Return the interface IP (the IP part from the CIDR) as IPv4
			ipAddr := interfaceIP.To4()
			if ipAddr != nil {
				log.Tracef("Found matching interface IP %s for target %s in CIDR %s", ipAddr.String(), targetIP, *cidr)
				return ipAddr.String(), nil
			}
		}
	}

	// If we had parse errors, include them in the final error message
	if len(parseErrors) > 0 {
		return "", fmt.Errorf("no matching network interface found for target IP %s. Parse errors encountered: %v", targetIP, parseErrors)
	}

	return "", fmt.Errorf("no matching network interface found for target IP %s in provided CIDR ranges: %v", targetIP, interfaceCIDRs)
}

// getAllNodeInterfaceIPs extracts all IP addresses from the node's network interface CIDRs
// and returns them as a comma-separated string. This is used as a fallback when direct
// network matching fails (e.g., for routed/indirect networks).
func getAllNodeInterfaceIPs(interfaceCIDRs []*string) (string, error) {
	log.Tracef(">>>>> getAllNodeInterfaceIPs with interfaceCIDRs: %v", interfaceCIDRs)
	defer log.Trace("<<<<< getAllNodeInterfaceIPs")

	if len(interfaceCIDRs) == 0 {
		return "", fmt.Errorf("interface CIDR list cannot be empty")
	}

	var ips []string
	var parseErrors []string

	for _, cidr := range interfaceCIDRs {
		if cidr == nil || *cidr == "" {
			continue // Skip empty CIDR entries
		}

		// Parse the CIDR to extract the IP address
		interfaceIP, _, err := net.ParseCIDR(*cidr)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("invalid CIDR format '%s': %v", *cidr, err))
			continue
		}

		// Convert to IPv4 if possible
		ipAddr := interfaceIP.To4()
		if ipAddr != nil {
			ips = append(ips, ipAddr.String())
		}
	}

	if len(ips) == 0 {
		if len(parseErrors) > 0 {
			return "", fmt.Errorf("no valid IP addresses found. Parse errors: %v", parseErrors)
		}
		return "", fmt.Errorf("no valid IP addresses found in provided CIDR ranges")
	}

	result := strings.Join(ips, ",")
	log.Tracef("Extracted IPs from node interfaces: %s", result)
	return result, nil
}

// generateRandomIPFromRange parses an IP range string in the format "10.132.230.12#24"
// and generates a random IP address within that range.
// The format means: starting from the base IP, there are 'range' number of consecutive IPs available.
// For example, "10.132.230.12#24" means IPs from 10.132.230.12 to 10.132.230.35 (24 IPs total).
func generateRandomIPFromRange(ipRangeStr string) (string, error) {
	log.Tracef(">>>>> generateRandomIPFromRange with ipRangeStr: %s", ipRangeStr)
	defer log.Trace("<<<<< generateRandomIPFromRange")

	if ipRangeStr == "" {
		return "", fmt.Errorf("IP range string cannot be empty")
	}

	// Format expected: "10.132.230.12#24" where 24 is the count of available IPs
	parts := strings.Split(ipRangeStr, "#")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid IP range format (expected 'IP#number'): %s", ipRangeStr)
	}

	baseIP := parts[0]
	rangeStr := parts[1]

	// Parse the range number (count of available IPs)
	rangeCount, err := strconv.Atoi(rangeStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse IP range count from '%s': %v", ipRangeStr, err)
	}

	if rangeCount <= 0 {
		return "", fmt.Errorf("IP range count must be positive: %d", rangeCount)
	}

	// Parse the base IP
	ipParts := strings.Split(baseIP, ".")
	if len(ipParts) != 4 {
		return "", fmt.Errorf("invalid IP format in range string: %s", baseIP)
	}

	// Extract IP prefix (first 3 octets) and the starting suffix (last octet)
	ipPrefix := strings.Join(ipParts[:3], ".")
	startSuffix, err := strconv.Atoi(ipParts[3])
	if err != nil {
		return "", fmt.Errorf("invalid last octet in base IP '%s': %v", baseIP, err)
	}

	// Generate random offset from 0 to (rangeCount - 1)
	randomOffset := rand.Intn(rangeCount)

	// Calculate the final IP suffix
	finalSuffix := startSuffix + randomOffset

	// Validate that we don't exceed 255 (max value for IP octet)
	if finalSuffix > 255 {
		return "", fmt.Errorf("calculated IP suffix %d exceeds maximum value 255 (base: %s, range: %d)",
			finalSuffix, baseIP, rangeCount)
	}

	// Create the generated IP
	generatedIP := fmt.Sprintf("%s.%d", ipPrefix, finalSuffix)
	log.Infof("Generated random IP %s from range %s (offset: %d from base %s)",
		generatedIP, ipRangeStr, randomOffset, baseIP)

	return generatedIP, nil
}

// ValidateFileVolumeConfig validates required configuration keys for file volumes
// and returns a map of validated string values. Performs special validation for hostIP.
func ValidateFileVolumeConfig(volume *model.Volume, volumeID string, requiredKeys ...string) (map[string]string, error) {
	if volume.Config == nil {
		return nil, fmt.Errorf("config is nil for volume %s", volumeID)
	}

	configValues := make(map[string]string)

	for _, key := range requiredKeys {
		value, ok := volume.Config[key]
		if !ok || value == nil {
			return nil, fmt.Errorf("%s key not found or value is nil in config for volume %s", key, volumeID)
		}

		stringValue, ok := value.(string)
		if !ok || stringValue == "" {
			return nil, fmt.Errorf("failed to get %s for volume %s", key, volumeID)
		}

		// Special validation for hostIP key
		if key == fileHostIPKey && !isValidIP(stringValue) {
			return nil, fmt.Errorf("invalid hostIP value for volume %s: %s", volumeID, stringValue)
		}

		configValues[key] = stringValue
	}

	return configValues, nil
}

// IsSnapshotSupportedByCSP checks if the given CSP service supports snapshot operations.
// Returns true if snapshots are supported, false otherwise.
func IsSnapshotSupportedByCSP(serviceName string) bool {
	// If serviceName is empty, assume snapshot support (default behavior)
	if serviceName == "" {
		return true
	}
	// Check if CSP is in the unsupported list
	return !snapshotUnsupportedCSPs[serviceName]
}
