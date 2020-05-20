// Copyright 2019 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	flavor *Flavor
)

func TestMain(m *testing.M) {
	clientSet, err := NewCluster(3)
	if err != nil {
		os.Exit(1)
	}
	flavor = &Flavor{kubeClient: clientSet}
	code := m.Run()
	os.Exit(code)
}

// New creates a fake K8s cluster
func NewCluster(nodes int) (*fake.Clientset, error) {
	clientset := fake.NewSimpleClientset()
	for i := 0; i < nodes; i++ {
		ready := v1.NodeCondition{Type: v1.NodeReady, Status: v1.ConditionTrue}
		name := fmt.Sprintf("node%d", i)
		n := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{nfsNodeSelectorKey: nfsNodeSelectorValue},
			},
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					ready,
				},
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeExternalIP,
						Address: fmt.Sprintf("%d.%d.%d.%d", i, i, i, i),
					},
				},
			},
		}
		_, err := clientset.CoreV1().Nodes().Create(n)
		if err != nil {
			return nil, err
		}

	}
	return clientset, nil
}

func TestGetNodes(t *testing.T) {
	nodes, err := flavor.getNFSNodes()
	assert.Nil(t, err)
	assert.NotNil(t, nodes)
	assert.Equal(t, len(nodes), 3)
}

func TestCreateNFSNamespace(t *testing.T) {
	namespace, err := flavor.createNFSNamespace(defaultNFSNamespace)
	assert.Nil(t, err)
	assert.Equal(t, namespace.ObjectMeta.Name, defaultNFSNamespace)
}

func TestCreateNFSService(t *testing.T) {
	err := flavor.createNFSService("hpe-nfs-my-service", defaultNFSNamespace)
	assert.Nil(t, err)
	service, err := flavor.getNFSService("hpe-nfs-my-service", defaultNFSNamespace)
	assert.Nil(t, err)
	assert.NotNil(t, service)
	assert.Equal(t, service.ObjectMeta.Name, "hpe-nfs-my-service")
	assert.Equal(t, v1.ServiceTypeClusterIP, service.Spec.Type)
}

func TestCreateServiceAccount(t *testing.T) {
	err := flavor.createServiceAccount(defaultNFSNamespace)
	assert.Nil(t, err)
	serviceAccount, err := flavor.kubeClient.CoreV1().ServiceAccounts(defaultNFSNamespace).Get(nfsServiceAccount, metav1.GetOptions{})
	assert.Nil(t, err)
	assert.Equal(t, serviceAccount.ObjectMeta.Name, nfsServiceAccount)
	// run duplicate call and make sure we don't throw an error if service account already exists
	err = flavor.createServiceAccount(defaultNFSNamespace)
	assert.Nil(t, err)
}

func TestCreateConfigMap(t *testing.T) {
	err := flavor.createNFSConfigMap(defaultNFSNamespace, "testdomain.com")
	assert.Nil(t, err)
	configMap, err := flavor.kubeClient.CoreV1().ConfigMaps(defaultNFSNamespace).Get(nfsConfigMap, metav1.GetOptions{})
	assert.Nil(t, err)
	assert.Equal(t, configMap.ObjectMeta.Name, nfsConfigMap)
}

func TestGetNFSSpec(t *testing.T) {
	createParams := make(map[string]string)

	// test with defaults
	spec, err := flavor.getNFSSpec(createParams)
	assert.Nil(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, defaultNFSImage, spec.image)
	assert.Nil(t, spec.resourceRequirements)
	assert.Equal(t, 1, len(spec.nodeSelector))

	// test with overrides
	createParams["nfsNamespace"] = "my-nfs-namespace"
	createParams["nfsProvisionerImage"] = "hpestorage/my-nfs-image:my-tag"
	createParams["nfsResourceLimitsCpuM"] = "500m"
	createParams["nfsResourceLimitsMemoryMi"] = "100Mi"

	spec, err = flavor.getNFSSpec(createParams)
	assert.Nil(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, spec.image, "hpestorage/my-nfs-image:my-tag")
	expectedCPU, _ := resource.ParseQuantity("500m")
	expectedMemory, _ := resource.ParseQuantity("100Mi")
	assert.Equal(t, spec.resourceRequirements.Limits[v1.ResourceCPU], expectedCPU)
	assert.Equal(t, spec.resourceRequirements.Limits[v1.ResourceMemory], expectedMemory)

	// test invalid cpu
	createParams["nfsResourceLimitsCpuM"] = "500x"
	spec, err = flavor.getNFSSpec(createParams)
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "invalid nfs cpu resource limit"))

	// test invalid memory
	createParams["nfsResourceLimitsCpuM"] = "500m"
	// Only suffixes: E, P, T, G, M, K and power-of-two equivalents: Ei, Pi, Ti, Gi, Mi, Ki are allowed
	createParams["nfsResourceLimitsMemoryMi"] = "100MB"
	spec, err = flavor.getNFSSpec(createParams)
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "invalid nfs memory resource limit"))
}
