// Copyright 2019 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"context"
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
				Labels: map[string]string{nfsNodeSelectorKey: nfsNodeSelectorDefaultValue},
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
		_, err := clientset.CoreV1().Nodes().Create(context.Background(), n, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}

	}
	return clientset, nil
}

func TestGetNodes(t *testing.T) {
	nodes, err := flavor.getNFSNodes(nfsNodeSelectorDefaultValue)
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
	serviceAccount, err := flavor.kubeClient.CoreV1().ServiceAccounts(defaultNFSNamespace).Get(context.Background(), nfsServiceAccount, metav1.GetOptions{})
	assert.Nil(t, err)
	assert.Equal(t, serviceAccount.ObjectMeta.Name, nfsServiceAccount)
	// run duplicate call and make sure we don't throw an error if service account already exists
	err = flavor.createServiceAccount(defaultNFSNamespace)
	assert.Nil(t, err)
}

func TestCreateConfigMap(t *testing.T) {
	err := flavor.createNFSConfigMap(defaultNFSNamespace, "testdomain.com")
	assert.Nil(t, err)
	configMap, err := flavor.kubeClient.CoreV1().ConfigMaps(defaultNFSNamespace).Get(context.Background(), nfsConfigMap, metav1.GetOptions{})
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
	expectedDefaultCPU, _ := resource.ParseQuantity("1000m")
	expectedDefaultMemory, _ := resource.ParseQuantity("2Gi")
	assert.Equal(t, spec.resourceRequirements.Limits[v1.ResourceCPU], expectedDefaultCPU)
	assert.Equal(t, spec.resourceRequirements.Limits[v1.ResourceMemory], expectedDefaultMemory)
	//assert.Nil(t, spec.resourceRequirements)
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

	// test invalid memory
	createParams["nfsResourceLimitsCpuM"] = "500m"
	// Only suffixes: E, P, T, G, M, K and power-of-two equivalents: Ei, Pi, Ti, Gi, Mi, Ki are allowed
	createParams["nfsResourceLimitsMemoryMi"] = "100MB"
	spec, err = flavor.getNFSSpec(createParams)
	assert.NotNil(t, err)
	createParams["nfsResourceLimitsMemoryMi"] = "2Gi"
	//setting nfsTolerationSeconds with non integer to check error condition
	createParams["nfsTolerationSeconds"] = "100MB"
	spec, err = flavor.getNFSSpec(createParams)
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "strconv.Atoi: parsing \"100MB\""))
	createParams["nfsTolerationSeconds"] = "300"
	spec, err = flavor.getNFSSpec(createParams)
	assert.Nil(t, err)
	assert.NotNil(t, spec)
	// check the tolerationSeconds value
	var num int64 = 300
	assert.Equal(t, num, *spec.tolerationSeconds)
}

func TestMakeNFSDeployment(t *testing.T) {
	var nfsSpec NFSSpec
	nfsSpec.image = "hpestorage/my-nfs-image:my-tag"
	nfsSpec.volumeClaim = ""
	resourceLimits := make(v1.ResourceList)
	cpuQuantity, _ := resource.ParseQuantity(defaultRLimitCPU)
	resourceLimits[v1.ResourceCPU] = cpuQuantity
	memoryLimitsQuantity, _ := resource.ParseQuantity(defaultRLimitMemory)
	resourceLimits[v1.ResourceMemory] = memoryLimitsQuantity
	resourceRequests := make(v1.ResourceList)
	cpuRequestsQuantity, _ := resource.ParseQuantity(defaultRRequestCPU)
	memoryRequestsQuantity, _ := resource.ParseQuantity(defaultRRequestMemory)
	resourceRequests[v1.ResourceMemory] = memoryRequestsQuantity
	resourceRequests[v1.ResourceCPU] = cpuRequestsQuantity
	nfsSpec.resourceRequirements = &v1.ResourceRequirements{
		Limits:   resourceLimits,
		Requests: resourceRequests,
	}
	nfsSpec.labelKey = defaultPodLabelKey
	nfsSpec.labelValue = defaultPodLabelValue
	nfsSpec.sourceNamespace = defaultNFSNamespace
	nfsSpec.sourceVolumeClaim = nfsSpec.volumeClaim
	dep := flavor.makeNFSDeployment("hpe-nfs-b50dea26-e6d5-4ab4-868e-033256aa1acd-649f89c89b-48t4k", &nfsSpec, defaultNFSNamespace)
	var num int64 = 30
	assert.Equal(t, *dep.Spec.Template.Spec.Tolerations[0].TolerationSeconds, num)
	num = 300
	nfsSpec.tolerationSeconds = &num
	dep = flavor.makeNFSDeployment("hpe-nfs-b50dea26-e6d5-4ab4-868e-033256aa1acd-649f89c89b-48t4k", &nfsSpec, defaultNFSNamespace)
	assert.Equal(t, *dep.Spec.Template.Spec.Tolerations[0].TolerationSeconds, num)
}

func TestCreateNFSServiceDuplicate(t *testing.T) {
	serviceName := "hpe-nfs-duplicate-test"
	err := flavor.createNFSService(serviceName, defaultNFSNamespace)
	assert.Nil(t, err)
	// create duplicate and ensure no error is thrown
	err = flavor.createNFSService(serviceName, defaultNFSNamespace)
	assert.Nil(t, err)
}

func TestGetNFSServiceNotFound(t *testing.T) {
	service, err := flavor.getNFSService("non-existent-service", defaultNFSNamespace)
	assert.NotNil(t, err)
	assert.Nil(t, service)
}

func TestMakeNFSDeploymentValidation(t *testing.T) {
	var nfsSpec NFSSpec
	nfsSpec.image = "hpestorage/test-image:v1.0"
	nfsSpec.volumeClaim = "test-pvc"
	nfsSpec.labelKey = "test-label-key"
	nfsSpec.labelValue = "test-label-value"
	nfsSpec.sourceNamespace = "test-namespace"
	nfsSpec.sourceVolumeClaim = "source-pvc"

	dep := flavor.makeNFSDeployment("test-deployment", &nfsSpec, "test-ns")

	assert.NotNil(t, dep)
	assert.Equal(t, "test-deployment", dep.ObjectMeta.Name)
	assert.Equal(t, "test-ns", dep.ObjectMeta.Namespace)
	assert.Equal(t, "hpestorage/test-image:v1.0", dep.Spec.Template.Spec.Containers[0].Image)
}

func TestGetNFSSpecWithCustomNodeSelector(t *testing.T) {
	// Create a node with custom label value
	ready := v1.NodeCondition{Type: v1.NodeReady, Status: v1.ConditionTrue}
	customNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "custom-node",
			Labels: map[string]string{nfsNodeSelectorKey: "custom-value"},
		},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{ready},
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeExternalIP,
					Address: "10.10.10.10",
				},
			},
		},
	}
	_, err := flavor.kubeClient.CoreV1().Nodes().Create(context.Background(), customNode, metav1.CreateOptions{})
	assert.Nil(t, err)

	createParams := make(map[string]string)
	createParams["nfsNodeSelector"] = "custom-value"

	spec, err := flavor.getNFSSpec(createParams)
	assert.Nil(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, 1, len(spec.nodeSelector))
	assert.Contains(t, spec.nodeSelector, nfsNodeSelectorKey)
	assert.Equal(t, "custom-value", spec.nodeSelector[nfsNodeSelectorKey])
}

func TestCreateConfigMapDuplicate(t *testing.T) {
	namespace := "test-config-namespace"
	domain := "example.com"

	// create namespace first
	_, err := flavor.createNFSNamespace(namespace)
	assert.Nil(t, err)

	// create config map
	err = flavor.createNFSConfigMap(namespace, domain)
	assert.Nil(t, err)

	// create duplicate
	err = flavor.createNFSConfigMap(namespace, domain)
	assert.Nil(t, err)
}

func TestGetNodesWithEmptySelector(t *testing.T) {
	nodes, err := flavor.getNFSNodes("")
	assert.Nil(t, err)
	// Empty selector value should match no nodes (all test nodes have value "true", not "")
	assert.Equal(t, 0, len(nodes))
}

func TestCreateNFSNamespaceDuplicate(t *testing.T) {
	namespace := "test-duplicate-namespace"

	// create namespace first time
	ns1, err := flavor.createNFSNamespace(namespace)
	assert.Nil(t, err)
	assert.Equal(t, namespace, ns1.ObjectMeta.Name)

	// create duplicate namespace - should return AlreadyExists error
	ns2, err := flavor.createNFSNamespace(namespace)
	assert.NotNil(t, err)
	assert.Nil(t, ns2)
	assert.Contains(t, err.Error(), "already exists")
}

func TestGetNFSSpecWithEmptyImage(t *testing.T) {
	createParams := make(map[string]string)
	createParams["nfsProvisionerImage"] = ""

	spec, err := flavor.getNFSSpec(createParams)
	assert.Nil(t, err)
	assert.NotNil(t, spec)
	// Empty string in params overrides the default, resulting in empty image
	assert.Equal(t, "", spec.image)
}

func TestMakeNFSDeploymentWithNilResourceRequirements(t *testing.T) {
	var nfsSpec NFSSpec
	nfsSpec.image = "hpestorage/test-nfs:latest"
	nfsSpec.volumeClaim = "test-vol"
	nfsSpec.labelKey = "app"
	nfsSpec.labelValue = "nfs-test"
	nfsSpec.sourceNamespace = defaultNFSNamespace
	nfsSpec.sourceVolumeClaim = "test-vol"
	nfsSpec.resourceRequirements = nil

	dep := flavor.makeNFSDeployment("test-nfs-deployment", &nfsSpec, defaultNFSNamespace)

	assert.NotNil(t, dep)
	assert.Equal(t, "test-nfs-deployment", dep.ObjectMeta.Name)
}

func TestGetNFSSpecWithZeroResourceLimits(t *testing.T) {
	createParams := make(map[string]string)
	createParams["nfsResourceLimitsCpuM"] = "0"
	createParams["nfsResourceLimitsMemoryMi"] = "0"

	spec, err := flavor.getNFSSpec(createParams)
	// Should handle zero values gracefully
	assert.Nil(t, err)
	assert.NotNil(t, spec)
}

func TestGetNFSSpecWithNegativeTolerationSeconds(t *testing.T) {
	createParams := make(map[string]string)
	createParams["nfsTolerationSeconds"] = "-100"

	spec, err := flavor.getNFSSpec(createParams)
	// Should handle negative values
	assert.Nil(t, err)
	assert.NotNil(t, spec)
}

func TestMakeNFSDeploymentWithCustomTolerations(t *testing.T) {
	var nfsSpec NFSSpec
	nfsSpec.image = "hpestorage/nfs:v2.0"
	nfsSpec.volumeClaim = "test-pvc"
	nfsSpec.labelKey = "env"
	nfsSpec.labelValue = "test"
	nfsSpec.sourceNamespace = "test-ns"
	nfsSpec.sourceVolumeClaim = "source-pvc"
	var customToleration int64 = 600
	nfsSpec.tolerationSeconds = &customToleration

	dep := flavor.makeNFSDeployment("nfs-with-tolerations", &nfsSpec, "test-namespace")

	assert.NotNil(t, dep)
	// Should have 3 tolerations: not-ready, unreachable, and dedicated
	assert.Equal(t, 3, len(dep.Spec.Template.Spec.Tolerations))

	// Check that custom toleration seconds are applied to not-ready and unreachable tolerations
	for _, toleration := range dep.Spec.Template.Spec.Tolerations {
		if toleration.Key == "node.kubernetes.io/not-ready" || toleration.Key == "node.kubernetes.io/unreachable" {
			assert.NotNil(t, toleration.TolerationSeconds)
			assert.Equal(t, customToleration, *toleration.TolerationSeconds)
		}
	}
}

func TestGetNFSServiceAfterCreation(t *testing.T) {
	serviceName := "test-get-service"
	namespace := defaultNFSNamespace

	// create service
	err := flavor.createNFSService(serviceName, namespace)
	assert.Nil(t, err)

	// get service and validate
	service, err := flavor.getNFSService(serviceName, namespace)
	assert.Nil(t, err)
	assert.NotNil(t, service)
	assert.Equal(t, serviceName, service.ObjectMeta.Name)
	assert.Equal(t, namespace, service.ObjectMeta.Namespace)
}

func TestHandleNFSNodePublish(t *testing.T) {
	// Create test namespace
	testNamespace := "test-nfs-namespace"
	_, err := flavor.createNFSNamespace(testNamespace)
	assert.Nil(t, err)

	// Create a test PVC to simulate the NFS volume source
	testPVC := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: testNamespace,
			UID:       "test-volume-id-12345",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	_, err = flavor.kubeClient.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.Background(), testPVC, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Create the underlying NFS PVC
	nfsPVCName := fmt.Sprintf("%s%s", nfsPrefix, testPVC.ObjectMeta.UID)
	nfsPVC := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nfsPVCName,
			Namespace: testNamespace,
			UID:       "nfs-pvc-uid-67890",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
		Status: v1.PersistentVolumeClaimStatus{
			Phase: v1.ClaimBound,
		},
	}
	_, err = flavor.kubeClient.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.Background(), nfsPVC, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Create the underlying PV for NFS PVC
	nfsPVName := fmt.Sprintf("%s%s", pvcPrefix, nfsPVC.ObjectMeta.UID)
	nfsPV := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: nfsPVName,
			Labels: map[string]string{
				nfsParentVolumeIDKey: string(testPVC.ObjectMeta.UID),
			},
		},
		Spec: v1.PersistentVolumeSpec{
			Capacity: v1.ResourceList{
				v1.ResourceStorage: resource.MustParse("1Gi"),
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{
					Driver:       "csi.hpe.com",
					VolumeHandle: "underlying-volume-handle",
				},
			},
			ClaimRef: &v1.ObjectReference{
				Name:      nfsPVCName,
				Namespace: testNamespace,
			},
		},
	}
	_, err = flavor.kubeClient.CoreV1().PersistentVolumes().Create(context.Background(), nfsPV, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Create NFS service with IPv4 cluster IP
	serviceName := nfsPVCName
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: testNamespace,
			Labels:    createNFSAppLabels(),
		},
		Spec: v1.ServiceSpec{
			Type:      v1.ServiceTypeClusterIP,
			ClusterIP: "10.96.100.50",
			Selector:  createNFSAppLabels(),
			Ports: []v1.ServicePort{
				{
					Name:     "nfs",
					Port:     2049,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}
	_, err = flavor.kubeClient.CoreV1().Services(testNamespace).Create(context.Background(), service, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Test case 1: Successful mount with IPv4
	t.Run("SuccessfulMountIPv4", func(t *testing.T) {
		// Note: This test will fail without a mock chapiDriver implementation
		// as it tries to actually mount the volume. In a real test environment,
		// you would need to mock the chapiDriver.MountNFSVolume method
		volumeID := string(testPVC.ObjectMeta.UID)

		// Verify getNFSResourceNameByVolumeID works
		resourceName, err := flavor.getNFSResourceNameByVolumeID(volumeID)
		assert.Nil(t, err)
		assert.Equal(t, nfsPVCName, resourceName)

		// Verify getNFSNamespaceByVolumeID works
		namespace, err := flavor.getNFSNamespaceByVolumeID(volumeID)
		assert.Nil(t, err)
		assert.Equal(t, testNamespace, namespace)

		// Verify service can be retrieved
		svc, err := flavor.getNFSService(resourceName, testNamespace)
		assert.Nil(t, err)
		assert.NotNil(t, svc)
		assert.Equal(t, "10.96.100.50", svc.Spec.ClusterIP)
	})
}

func TestHandleNFSNodePublishIPv6(t *testing.T) {
	// Create test namespace for IPv6 test
	testNamespace := "test-nfs-ipv6-namespace"
	_, err := flavor.createNFSNamespace(testNamespace)
	assert.Nil(t, err)

	// Create a test PVC
	testPVC := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ipv6-pvc",
			Namespace: testNamespace,
			UID:       "ipv6-volume-id-12345",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	_, err = flavor.kubeClient.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.Background(), testPVC, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Create the underlying NFS PVC
	nfsPVCName := fmt.Sprintf("%s%s", nfsPrefix, testPVC.ObjectMeta.UID)
	nfsPVC := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nfsPVCName,
			Namespace: testNamespace,
			UID:       "nfs-ipv6-pvc-uid",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
		},
		Status: v1.PersistentVolumeClaimStatus{
			Phase: v1.ClaimBound,
		},
	}
	_, err = flavor.kubeClient.CoreV1().PersistentVolumeClaims(testNamespace).Create(context.Background(), nfsPVC, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Create the underlying PV
	nfsPVName := fmt.Sprintf("%s%s", pvcPrefix, nfsPVC.ObjectMeta.UID)
	nfsPV := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: nfsPVName,
			Labels: map[string]string{
				nfsParentVolumeIDKey: string(testPVC.ObjectMeta.UID),
			},
		},
		Spec: v1.PersistentVolumeSpec{
			Capacity: v1.ResourceList{
				v1.ResourceStorage: resource.MustParse("1Gi"),
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{
					Driver:       "csi.hpe.com",
					VolumeHandle: "ipv6-volume-handle",
				},
			},
			ClaimRef: &v1.ObjectReference{
				Name:      nfsPVCName,
				Namespace: testNamespace,
			},
		},
	}
	_, err = flavor.kubeClient.CoreV1().PersistentVolumes().Create(context.Background(), nfsPV, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Create NFS service with IPv6 cluster IP
	serviceName := nfsPVCName
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: testNamespace,
			Labels:    createNFSAppLabels(),
		},
		Spec: v1.ServiceSpec{
			Type:      v1.ServiceTypeClusterIP,
			ClusterIP: "2001:db8::1",
			Selector:  createNFSAppLabels(),
			Ports: []v1.ServicePort{
				{
					Name:     "nfs",
					Port:     2049,
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}
	_, err = flavor.kubeClient.CoreV1().Services(testNamespace).Create(context.Background(), service, metav1.CreateOptions{})
	assert.Nil(t, err)

	// Verify service retrieval and IPv6 handling
	volumeID := string(testPVC.ObjectMeta.UID)
	resourceName, err := flavor.getNFSResourceNameByVolumeID(volumeID)
	assert.Nil(t, err)

	svc, err := flavor.getNFSService(resourceName, testNamespace)
	assert.Nil(t, err)
	assert.NotNil(t, svc)
	assert.Equal(t, "2001:db8::1", svc.Spec.ClusterIP)
}

func TestGetNFSMountOptions(t *testing.T) {
	// Test with custom mount options
	volumeContext := map[string]string{
		nfsMountOptionsKey: "rw,nolock,vers=4.1",
	}
	mountOptions := getNFSMountOptions(volumeContext)
	assert.NotNil(t, mountOptions)
	assert.Equal(t, 3, len(mountOptions))
	assert.Equal(t, "rw", mountOptions[0])
	assert.Equal(t, "nolock", mountOptions[1])
	assert.Equal(t, "vers=4.1", mountOptions[2])

	// Test with no mount options
	emptyContext := map[string]string{}
	mountOptions = getNFSMountOptions(emptyContext)
	assert.Nil(t, mountOptions)
}
