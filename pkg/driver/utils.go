// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

// IP address family identifiers returned by ipFamily.
const (
	ipv4Family = "ipv4"
	ipv6Family = "ipv6"
)

// ipFamily classifies an address as IPv4 or IPv6. It returns ipv4Family or
// ipv6Family for a parseable address, or "" when the string is not a valid IP
// address. Surrounding brackets used for IPv6 literals (e.g. "[fd00::1]") are
// tolerated so addresses taken from URLs/host:port forms still classify.
func ipFamily(addr string) string {
	addr = strings.Trim(strings.TrimSpace(addr), "[]")
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		return ipv4Family
	}
	return ipv6Family
}

// validateNoDualStack returns an error if the provided addresses contain a mix
// of IPv4 and IPv6 families. Dual-stack (mixed IPv4/IPv6) backends are not
// supported (CROSS-1); all backend addresses must belong to a single address
// family. Empty input and non-IP / unparseable entries are ignored so that
// protocols without discovery IPs (e.g. FC) and malformed-but-harmless entries
// do not trigger false rejections.
func validateNoDualStack(addresses []string) error {
	hasIPv4, hasIPv6 := false, false
	for _, addr := range addresses {
		switch ipFamily(addr) {
		case ipv4Family:
			hasIPv4 = true
		case ipv6Family:
			hasIPv6 = true
		}
	}
	if hasIPv4 && hasIPv6 {
		return fmt.Errorf("dual-stack (mixed IPv4/IPv6) is not supported; "+
			"ensure all backend addresses use the same address family: %v", addresses)
	}
	return nil
}

// validateProtocolDiscoveryIPs enforces protocol-specific IP family
// constraints on discovery addresses. NVMe/TCP supports IPv4 discovery
// addresses only (CROSS-1), so any IPv6 discovery address is rejected for that
// protocol. Other protocols are unconstrained here. Non-IP / unparseable
// entries are ignored.
func validateProtocolDiscoveryIPs(protocol string, addresses []string) error {
	if !strings.EqualFold(protocol, nvmetcp) {
		return nil
	}
	for _, addr := range addresses {
		if ipFamily(addr) == ipv6Family {
			return fmt.Errorf("NVMe/TCP only supports IPv4 discovery addresses; got IPv6 address %q", addr)
		}
	}
	return nil
}

// isReplicationRequested reports whether the supplied StorageClass/PVC create
// parameters request array-based replication (Remote Copy). Replication is
// considered requested when any of the replication-related keys is present with
// a non-empty / truthy value:
//
//   - remoteCopyGroup: a non-empty Remote Copy Group name.
//   - replicationDevices: a non-empty replication-device CRD reference.
//   - oneRcgPerPvc: "true" (CSP derives the RCG name from the PVC name).
//   - allowBatchReplicatedVolumeCreation: "true" (batch replicated create).
//
// Keys present but empty/false do not request replication. This mirrors the
// CSP-side semantics where a non-empty RemoteCopyGroup/ReplicationDevices or a
// true OneRcgPerPvc drives the replicated code path.
func isReplicationRequested(createParameters map[string]string) bool {
	if createParameters == nil {
		return false
	}
	if strings.TrimSpace(createParameters[remoteCopyGroupKey]) != "" {
		return true
	}
	if strings.TrimSpace(createParameters[replicationDevicesKey]) != "" {
		return true
	}
	if isTrue(createParameters[oneRcgPerPvcKey]) {
		return true
	}
	if isTrue(createParameters[allowBatchReplicatedVolumeCreationKey]) {
		return true
	}
	return false
}

// isTrue parses a StorageClass parameter value as a boolean, tolerating
// surrounding whitespace and mixed case. Unparseable values are treated as
// false so a malformed flag never accidentally enables a code path.
func isTrue(val string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(val))
	return err == nil && b
}

// validateReplicationProtocolCompatibility rejects volume create requests that
// combine array-based replication with the NVMe/TCP access protocol, which is
// supported on standalone arrays only (CROSS-2). The check runs in the CSI
// driver so a misconfigured StorageClass fails fast with a clear message
// instead of being forwarded to the CSP where replication setup would fail or
// partially succeed. The CSP performs the equivalent validation as defense in
// depth.
//
// The protocol is normalized through resolveAccessProtocol so that the
// "nvmeotcp" synonym and an empty (defaulted) value are handled consistently
// (CROSS-3). An invalid protocol is reported by resolveAccessProtocol's error.
func validateReplicationProtocolCompatibility(createParameters map[string]string) error {
	if !isReplicationRequested(createParameters) {
		return nil
	}
	protocol, err := resolveAccessProtocol(createParameters[accessProtocolKey])
	if err != nil {
		return err
	}
	if protocol == nvmetcp {
		return status.Error(codes.InvalidArgument,
			"Provisioning of replicated volume is not supported with nvmetcp; "+
				"NVMe/TCP is supported on standalone arrays only. Remove the replication "+
				"parameters (remoteCopyGroup, replicationDevices, oneRcgPerPvc, "+
				"allowBatchReplicatedVolumeCreation) or choose a different access protocol (iscsi, fc)")
	}
	return nil
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
