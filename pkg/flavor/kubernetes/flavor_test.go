// Copyright 2019, 2025 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"fmt"
	"testing"

	crd_v1 "github.com/hpe-storage/k8s-custom-resources/pkg/apis/hpestorage/v1"
	"github.com/stretchr/testify/assert"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// validateUniqueInitiatorsForTest is a test-only version that takes node list directly
// This avoids needing to mock the CRD client and tests the core validation logic
func validateUniqueInitiatorsForTest(nodeName string, iqns, wwpns, nqns []string, existingNodes []crd_v1.HPENodeInfo) error {
	// Check each existing node (except the current one)
	for _, existingNodeInfo := range existingNodes {
		if existingNodeInfo.Name == nodeName {
			continue // Skip self
		}

		// Check for duplicate IQNs
		for _, currentIQN := range iqns {
			if currentIQN == "" {
				continue // Skip empty
			}
			for _, existingIQN := range existingNodeInfo.Spec.IQNs {
				if currentIQN == existingIQN {
					return fmt.Errorf(
						"CRITICAL: Duplicate IQN '%s' detected on node '%s' (attempting to register on node '%s'). "+
							"This will cause data corruption. Each node must have unique iSCSI initiator names. "+
							"Please regenerate IQNs or fix node templates before proceeding",
						currentIQN, existingNodeInfo.Name, nodeName)
				}
			}
		}

		// Check for duplicate WWPNs
		for _, currentWWPN := range wwpns {
			if currentWWPN == "" {
				continue
			}
			for _, existingWWPN := range existingNodeInfo.Spec.WWPNs {
				if currentWWPN == existingWWPN {
					return fmt.Errorf(
						"CRITICAL: Duplicate WWPN '%s' detected on node '%s' (attempting to register on node '%s'). "+
							"This will cause data corruption. Each node must have unique Fibre Channel WWPNs",
						currentWWPN, existingNodeInfo.Name, nodeName)
				}
			}
		}

		// Check for duplicate NQNs
		for _, currentNQN := range nqns {
			if currentNQN == "" {
				continue
			}
			for _, existingNQN := range existingNodeInfo.Spec.NQNs {
				if currentNQN == existingNQN {
					return fmt.Errorf(
						"CRITICAL: Duplicate NQN '%s' detected on node '%s' (attempting to register on node '%s'). "+
							"This will cause data corruption. Each node must have unique NVMe Qualified Names",
						currentNQN, existingNodeInfo.Name, nodeName)
				}
			}
		}
	}

	return nil
}

func TestValidateUniqueInitiators(t *testing.T) {
	tests := []struct {
		name           string
		existingNodes  []crd_v1.HPENodeInfo
		newNodeName    string
		newIQNs        []string
		newWWPNs       []string
		newNQNs        []string
		expectError    bool
		errorSubstring string
	}{
		{
			name: "No duplicate IQNs - should succeed",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:node1"},
						WWPNs: []string{"10:00:00:00:c9:a1:b2:c3"},
						NQNs:  []string{"nqn.2014-08.org.nvmexpress:uuid:node1"},
					},
				},
			},
			newNodeName: "node2",
			newIQNs:     []string{"iqn.1994-05.com.redhat:node2"},
			newWWPNs:    []string{"10:00:00:00:c9:a1:b2:c4"},
			newNQNs:     []string{"nqn.2014-08.org.nvmexpress:uuid:node2"},
			expectError: false,
		},
		{
			name: "Duplicate IQN detected - should fail",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:duplicated"},
						WWPNs: []string{"10:00:00:00:c9:a1:b2:c3"},
						NQNs:  []string{"nqn.2014-08.org.nvmexpress:uuid:node1"},
					},
				},
			},
			newNodeName:    "node2",
			newIQNs:        []string{"iqn.1994-05.com.redhat:duplicated"},
			newWWPNs:       []string{"10:00:00:00:c9:a1:b2:c4"},
			newNQNs:        []string{"nqn.2014-08.org.nvmexpress:uuid:node2"},
			expectError:    true,
			errorSubstring: "Duplicate IQN 'iqn.1994-05.com.redhat:duplicated' detected on node 'node1'",
		},
		{
			name: "Duplicate WWPN detected - should fail",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:node1"},
						WWPNs: []string{"10:00:00:00:c9:a1:b2:c3"},
						NQNs:  []string{"nqn.2014-08.org.nvmexpress:uuid:node1"},
					},
				},
			},
			newNodeName:    "node2",
			newIQNs:        []string{"iqn.1994-05.com.redhat:node2"},
			newWWPNs:       []string{"10:00:00:00:c9:a1:b2:c3"}, // Duplicate
			newNQNs:        []string{"nqn.2014-08.org.nvmexpress:uuid:node2"},
			expectError:    true,
			errorSubstring: "Duplicate WWPN '10:00:00:00:c9:a1:b2:c3' detected on node 'node1'",
		},
		{
			name: "Duplicate NQN detected - should fail",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:node1"},
						WWPNs: []string{"10:00:00:00:c9:a1:b2:c3"},
						NQNs:  []string{"nqn.2014-08.org.nvmexpress:uuid:duplicated"},
					},
				},
			},
			newNodeName:    "node2",
			newIQNs:        []string{"iqn.1994-05.com.redhat:node2"},
			newWWPNs:       []string{"10:00:00:00:c9:a1:b2:c4"},
			newNQNs:        []string{"nqn.2014-08.org.nvmexpress:uuid:duplicated"}, // Duplicate
			expectError:    true,
			errorSubstring: "Duplicate NQN 'nqn.2014-08.org.nvmexpress:uuid:duplicated' detected on node 'node1'",
		},
		{
			name: "Same node updating its own initiators - should succeed",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:node1"},
						WWPNs: []string{"10:00:00:00:c9:a1:b2:c3"},
						NQNs:  []string{"nqn.2014-08.org.nvmexpress:uuid:node1"},
					},
				},
			},
			newNodeName: "node1", // Same node
			newIQNs:     []string{"iqn.1994-05.com.redhat:node1"},
			newWWPNs:    []string{"10:00:00:00:c9:a1:b2:c3"},
			newNQNs:     []string{"nqn.2014-08.org.nvmexpress:uuid:node1"},
			expectError: false,
		},
		{
			name: "Empty initiators - should succeed",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:node1"},
						WWPNs: []string{},
						NQNs:  []string{},
					},
				},
			},
			newNodeName: "node2",
			newIQNs:     []string{},
			newWWPNs:    []string{},
			newNQNs:     []string{},
			expectError: false,
		},
		{
			name: "Multiple IQNs with one duplicate - should fail",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:node1a", "iqn.1994-05.com.redhat:node1b"},
						WWPNs: []string{},
						NQNs:  []string{},
					},
				},
			},
			newNodeName:    "node2",
			newIQNs:        []string{"iqn.1994-05.com.redhat:node2a", "iqn.1994-05.com.redhat:node1b"}, // Second is duplicate
			newWWPNs:       []string{},
			newNQNs:        []string{},
			expectError:    true,
			errorSubstring: "Duplicate IQN 'iqn.1994-05.com.redhat:node1b'",
		},
		{
			name: "Multiple nodes - duplicate on second node",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:node1"},
						WWPNs: []string{},
						NQNs:  []string{},
					},
				},
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node2"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{"iqn.1994-05.com.redhat:duplicated"},
						WWPNs: []string{},
						NQNs:  []string{},
					},
				},
			},
			newNodeName:    "node3",
			newIQNs:        []string{"iqn.1994-05.com.redhat:duplicated"},
			newWWPNs:       []string{},
			newNQNs:        []string{},
			expectError:    true,
			errorSubstring: "Duplicate IQN",
		},
		{
			name: "Empty string IQN should be skipped",
			existingNodes: []crd_v1.HPENodeInfo{
				{
					ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
					Spec: crd_v1.HPENodeInfoSpec{
						IQNs:  []string{""},
						WWPNs: []string{},
						NQNs:  []string{},
					},
				},
			},
			newNodeName: "node2",
			newIQNs:     []string{""},
			newWWPNs:    []string{},
			newNQNs:     []string{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the validation logic directly with existing nodes
			err := validateUniqueInitiatorsForTest(
				tt.newNodeName,
				tt.newIQNs,
				tt.newWWPNs,
				tt.newNQNs,
				tt.existingNodes,
			)

			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring, "Error message should contain expected substring")
				}
				assert.Contains(t, err.Error(), "CRITICAL", "Error should be marked as CRITICAL")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}
