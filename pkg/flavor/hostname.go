// Copyright 2026 Hewlett Packard Enterprise Development LP

package flavor

import (
	"crypto/sha256"
	"fmt"
)

const (
	// DisableHostnameEnvKey opts a node in to registering a hashed hostname with the backend.
	DisableHostnameEnvKey = "DISABLE_HOSTNAME"

	// MaxHostnameLenEnvKey overrides DefaultMaxHostnameLen when DisableHostnameEnvKey is set.
	MaxHostnameLenEnvKey = "MAX_HOSTNAME_LEN"

	// DefaultMaxHostnameLen is the platform's naming limit, not our own name's budget.
	DefaultMaxHostnameLen = 31

	// BackendGeneratedHostnameAnnotationKey stores the backend-generated hostname, separate from ObjectMeta.Name.
	BackendGeneratedHostnameAnnotationKey = "storage.hpe.com/backend-generated-hostname"

	sanitizedHostnamePrefix = "csi-"

	// cspProtocolPrefixReserve covers the longest CSP-added prefix ("nqntcp-" for NVMe/TCP).
	cspProtocolPrefixReserve = len("nqntcp-")
)

// SanitizeHostname always hashes the FQDN+UUID so every opted-in node follows the same
// naming convention, reserving room in maxLen for the CSP's own protocol prefix.
func SanitizeHostname(fqdn, uuid string, maxLen int) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(fqdn+uuid)))
	hashLen := maxLen - cspProtocolPrefixReserve - len(sanitizedHostnamePrefix)
	if hashLen < 0 {
		hashLen = 0
	}
	if hashLen > len(hash) {
		hashLen = len(hash)
	}
	return sanitizedHostnamePrefix + hash[:hashLen]
}
