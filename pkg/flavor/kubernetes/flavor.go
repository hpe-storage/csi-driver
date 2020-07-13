// Copyright 2019 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"crypto/sha256"
	b64 "encoding/base64"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/hpe-storage/common-host-libs/chapi"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	crd_v1 "github.com/hpe-storage/k8s-custom-resources/pkg/apis/hpestorage/v1"
	crd_client "github.com/hpe-storage/k8s-custom-resources/pkg/client/clientset/versioned"
	v1 "k8s.io/api/core/v1"
	storage_v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	core_v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

const (
	allowOverrides       = "allowOverrides"
	nodeUnreachableTaint = "node.kubernetes.io/unreachable"
	nodeLostCondition    = "NodeLost"
)

var (
	// resyncPeriod describes how often to get a full resync (0=never)
	resyncPeriod = 5 * time.Minute
)

// Flavor of the CSI driver
type Flavor struct {
	crdClient   *crd_client.Clientset
	nodeName    string
	kubeClient  kubernetes.Interface
	chapiDriver chapi.Driver

	claimInformer cache.SharedIndexInformer
	claimIndexer  cache.Indexer
	claimStopChan chan struct{}

	eventRecorder record.EventRecorder
}

// NewKubernetesFlavor creates a new k8s flavored CSI driver
func NewKubernetesFlavor(nodeService bool, chapiDriver chapi.Driver) (*Flavor, error) {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		fmt.Printf("Error getting config cluster - %s\n", err.Error())
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		fmt.Printf("Error getting client - %s\n", err.Error())
		return nil, err
	}

	crdClient, err := crd_client.NewForConfig(kubeConfig)
	if err != nil {
		fmt.Printf("Error getting crd client - %s\n", err.Error())
		os.Exit(1)
	}

	flavor := &Flavor{
		kubeClient: kubeClient,
		crdClient:  crdClient,
	}

	if !nodeService {
		broadcaster := record.NewBroadcaster()
		broadcaster.StartRecordingToSink(&core_v1.EventSinkImpl{Interface: kubeClient.CoreV1().Events(v1.NamespaceAll)})
		flavor.eventRecorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "csi.hpe.com"})

		claimListWatch := &cache.ListWatch{
			ListFunc:  flavor.listAllClaims,
			WatchFunc: flavor.watchAllClaims,
		}

		flavor.claimInformer = cache.NewSharedIndexInformer(
			claimListWatch,
			&v1.PersistentVolumeClaim{},
			resyncPeriod,
			cache.Indexers{
				"uid": MetaUIDFunc,
			},
		)
		flavor.claimIndexer = flavor.claimInformer.GetIndexer()
		flavor.claimStopChan = make(chan struct{})
		go flavor.claimInformer.Run(flavor.claimStopChan)
	}

	// add reference to chapi driver
	flavor.chapiDriver = chapiDriver

	return flavor, nil
}

// ConfigureAnnotations takes the PVC annotations and overrides any parameters in the CSI create volume request
func (flavor *Flavor) ConfigureAnnotations(name string, parameters map[string]string) (map[string]string, error) {
	log.Tracef(">>>>> ConfigureAnnotations called with PVC Name %s", name)
	defer log.Trace("<<<<< ConfigureAnnotations")

	pvc, err := flavor.getClaimFromClaimName(name)
	if err != nil {
		return nil, err
	}
	log.Infof("Configuring annotations on PVC %v", pvc)

	overrideKeys := flavor.getClassOverrideOptions(parameters)

	// get updated options map for handling overrides and annotations
	parameters, err = flavor.getClaimOverrideOptions(pvc, overrideKeys, parameters, "csi.hpe.com")
	if err != nil {
		flavor.eventRecorder.Event(pvc, v1.EventTypeWarning, "ProvisionStorage", err.Error())
		log.Errorf("error handling annotations. err=%v", err)
	}

	return parameters, nil
}

// LoadNodeInfo will load a node as an HPENodeInfo CRD object
func (flavor *Flavor) LoadNodeInfo(node *model.Node) (string, error) {
	log.Tracef(">>>>> LoadNodeInfo called with node %v", node)
	defer log.Trace("<<<<< LoadNodeInfo")

	// overwrite actual host name with node name from k8s to be compliant
	if name := os.Getenv("NODE_NAME"); name != "" {
		node.Name = name
	}

	nodeInfo, err := flavor.getNodeInfoByUUID(node.UUID)
	if err != nil {
		log.Errorf("Error obtaining node info by uuid %s- %s\n", node.UUID, err.Error())
		return "", err
	}

	if nodeInfo == nil {
		nodeInfo, err = flavor.getNodeInfoByName(node.Name)
		if err != nil {
			log.Errorf("Error obtaining node info by name %s- %s\n", node.Name, err.Error())
			return "", err
		}
	}

	if nodeInfo != nil {
		// update nodename for lookup during cleanup(unload)
		flavor.nodeName = nodeInfo.ObjectMeta.Name

		log.Infof("Node info %s already known to cluster\n", nodeInfo.ObjectMeta.Name)
		// make sure the nodeInfo has updated information from the host
		updateNodeRequired := false

		// update node uuid on mismatch
		if nodeInfo.Spec.UUID != node.UUID {
			nodeInfo.Spec.UUID = node.UUID
			updateNodeRequired = true
		}
		// update node initiator IQNs on mismatch
		iqnsFromNode := getIqnsFromNode(node)
		if !reflect.DeepEqual(nodeInfo.Spec.IQNs, iqnsFromNode) {
			nodeInfo.Spec.IQNs = iqnsFromNode
			updateNodeRequired = true
		}
		// update node network information on mismatch
		networksFromNode := getNetworksFromNode(node)
		if !reflect.DeepEqual(nodeInfo.Spec.Networks, networksFromNode) {
			nodeInfo.Spec.Networks = networksFromNode
			updateNodeRequired = true
		}
		// update node FC port WWPNs on mismatch
		wwpnsFromNode := getWwpnsFromNode(node)
		if !reflect.DeepEqual(nodeInfo.Spec.WWPNs, wwpnsFromNode) {
			nodeInfo.Spec.WWPNs = wwpnsFromNode
			updateNodeRequired = true
		}

		if !updateNodeRequired {
			// no update needed to existing CRD
			return node.UUID, nil
		}
		log.Infof("updating Node %s with iqns %v wwpns %v networks %v",
			nodeInfo.Name, nodeInfo.Spec.IQNs, nodeInfo.Spec.WWPNs, nodeInfo.Spec.Networks)
		_, err := flavor.crdClient.StorageV1().HPENodeInfos().Update(nodeInfo)
		if err != nil {
			log.Errorf("Error updating the node %s - %s\n", nodeInfo.Name, err.Error())
			return "", err
		}
	} else {
		// if we didn't find HPENodeInfo yet, create one.
		newNodeInfo := &crd_v1.HPENodeInfo{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: node.Name,
			},
			Spec: crd_v1.HPENodeInfoSpec{
				UUID:         node.UUID,
				IQNs:         getIqnsFromNode(node),
				Networks:     getNetworksFromNode(node),
				WWPNs:        getWwpnsFromNode(node),
				ChapUser:     node.ChapUser,
				ChapPassword: b64.StdEncoding.EncodeToString([]byte(node.ChapPassword)),
			},
		}

		log.Infof("Adding node with name %s", node.Name)
		nodeInfo, err = flavor.crdClient.StorageV1().HPENodeInfos().Create(newNodeInfo)
		if err != nil {
			log.Fatalf("Error adding node %v - %s", nodeInfo, err.Error())
		}

		// update nodename for lookup during cleanup(unload)
		flavor.nodeName = nodeInfo.ObjectMeta.Name
		log.Infof("Successfully added node info for node %v", nodeInfo)
	}

	return node.UUID, nil
}

func getIqnsFromNode(node *model.Node) []string {
	var iqns []string
	for i := 0; i < len(node.Iqns); i++ {
		iqns = append(iqns, *node.Iqns[i])
	}
	return iqns
}

func getWwpnsFromNode(node *model.Node) []string {
	var wwpns []string
	for i := 0; i < len(node.Wwpns); i++ {
		wwpns = append(wwpns, *node.Wwpns[i])
	}
	return wwpns
}

func getNetworksFromNode(node *model.Node) []string {
	var networks []string
	for i := 0; i < len(node.Networks); i++ {
		networks = append(networks, *node.Networks[i])
	}
	return networks
}

// UnloadNodeInfo remove the HPENodeInfo from the list of CRDs
func (flavor *Flavor) UnloadNodeInfo() {
	log.Tracef(">>>>>> UnloadNodeInfo with name %s", flavor.nodeName)
	defer log.Trace("<<<<<< UnloadNodeInfo")

	err := flavor.crdClient.StorageV1().HPENodeInfos().Delete(flavor.nodeName, &meta_v1.DeleteOptions{})
	if err != nil {
		log.Errorf("Failed to delete node %s - %s", flavor.nodeName, err.Error())
	}
}

// GetNodeInfo retrieves the Node identified by nodeID from the list of CRDs
func (flavor *Flavor) GetNodeInfo(nodeID string) (*model.Node, error) {
	log.Tracef(">>>>>> GetNodeInfo from node ID %s", nodeID)
	defer log.Trace("<<<<<< GetNodeInfo")

	nodeInfoList, err := flavor.crdClient.StorageV1().HPENodeInfos().List(meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	log.Tracef("Found the following HPE Node Info objects: %v", nodeInfoList)

	for _, nodeInfo := range nodeInfoList.Items {
		log.Tracef("Processing node info %v", nodeInfo)

		if nodeInfo.Spec.UUID == nodeID {
			iqns := make([]*string, len(nodeInfo.Spec.IQNs))
			for i := range iqns {
				iqns[i] = &nodeInfo.Spec.IQNs[i]
			}
			networks := make([]*string, len(nodeInfo.Spec.Networks))
			for i := range networks {
				networks[i] = &nodeInfo.Spec.Networks[i]
			}
			wwpns := make([]*string, len(nodeInfo.Spec.WWPNs))
			for i := range wwpns {
				wwpns[i] = &nodeInfo.Spec.WWPNs[i]
			}
			node := &model.Node{
				Name:         nodeInfo.ObjectMeta.Name,
				UUID:         nodeInfo.Spec.UUID,
				Iqns:         iqns,
				Networks:     networks,
				Wwpns:        wwpns,
				ChapUser:     nodeInfo.Spec.ChapUser,
				ChapPassword: nodeInfo.Spec.ChapPassword,
			}

			return node, nil
		}
	}

	return nil, fmt.Errorf("failed to get node with id %s", nodeID)
}

//NewClaimController provides a controller that watches for PersistentVolumeClaims and takes action on them
//nolint: dupl
func (flavor *Flavor) newClaimIndexer() cache.Indexer {
	claimListWatch := &cache.ListWatch{
		ListFunc:  flavor.listAllClaims,
		WatchFunc: flavor.watchAllClaims,
	}

	informer := cache.NewSharedIndexInformer(
		claimListWatch,
		&v1.PersistentVolumeClaim{},
		resyncPeriod,
		cache.Indexers{
			"uid": MetaUIDFunc,
		},
	)

	return informer.GetIndexer()
}

func (flavor *Flavor) listAllClaims(options meta_v1.ListOptions) (runtime.Object, error) {
	return flavor.kubeClient.CoreV1().PersistentVolumeClaims(meta_v1.NamespaceAll).List(options)
}

func (flavor *Flavor) watchAllClaims(options meta_v1.ListOptions) (watch.Interface, error) {
	return flavor.kubeClient.CoreV1().PersistentVolumeClaims(meta_v1.NamespaceAll).Watch(options)
}

// get the pv corresponding to this pvc and substitute with pv (docker/csi volume name)
func (flavor *Flavor) getVolumeNameFromClaimName(name string) (string, error) {
	log.Tracef(">>>>> getVolumeNameFromClaimName called with PVC Name %s", name)
	defer log.Trace("<<<<< getVolumeNameFromClaimName")

	claim, err := flavor.getClaimFromClaimName(name)
	if err != nil {
		return "", err
	}
	if claim == nil || claim.Spec.VolumeName == "" {
		return "", fmt.Errorf("no volume found for claim %s", name)
	}
	return claim.Spec.VolumeName, nil
}

func (flavor *Flavor) getClaimFromClaimName(name string) (*v1.PersistentVolumeClaim, error) {
	log.Tracef(">>>>> getClaimFromClaimName called with %s", name)
	defer log.Trace("<<<<< getClaimFromClaimName")

	if flavor.claimIndexer == nil {
		return nil, fmt.Errorf("requested pvc %s was not found because claimIndexer was nil", name)
	}

	uid := name[4:len(name)]
	log.Infof("Looking up PVC with uid %s", uid)

	claims, err := flavor.claimIndexer.ByIndex("uid", uid)
	if err != nil {
		return nil, fmt.Errorf("Failed to retrieve claim by uid %s", uid)
	} else if len(claims) == 0 {
		return nil, fmt.Errorf("Requested pvc %s was not found with uid %s", name, uid)
	}

	log.Infof("Found the following claims: %v", claims)

	return claims[0].(*v1.PersistentVolumeClaim), nil
}

func (flavor *Flavor) getClassOverrideOptions(optionsMap map[string]string) []string {
	log.Trace(">>>> getClassOverrideOptions")
	defer log.Trace("<<<<< getClassOverrideOptions")

	var overridekeys []string
	if val, ok := optionsMap[allowOverrides]; ok {
		log.Infof("allowOverrides %s", val)
		for _, v := range strings.Split(val, ",") {
			// remove leading and trailing spaces from value before Trim (needed to support multiline overrides e.g ", ")
			v = strings.TrimSpace(v)
			if len(v) > 0 && v != "" {
				log.Infof("processing key: %v", v)
				overridekeys = append(overridekeys, v)
			}
		}
	}

	// Always let nfsPVC option to be overridden for underlying PVC in case of NFS provisioning
	overridekeys = append(overridekeys, "nfsPVC")

	log.Infof("resulting override keys :%#v", overridekeys)
	return overridekeys
}

func (flavor *Flavor) getClaimOverrideOptions(claim *v1.PersistentVolumeClaim, overrides []string, optionsMap map[string]string, provisioner string) (map[string]string, error) {
	log.Tracef(">>>>> getClaimOverrideOptions for %s", provisioner)
	defer log.Trace("<<<<< getClaimOverrideOptions")

	provisionerName := provisioner
	for _, override := range overrides {
		for key, annotation := range claim.Annotations {
			if strings.HasPrefix(key, provisionerName+"/"+override) {
				if valOpt, ok := optionsMap[override]; ok {
					if override == "size" || override == "sizeInGiB" {
						// do not allow  override of size and sizeInGiB
						log.Infof("override of size and sizeInGiB is not permitted, default to claim capacity of %v", valOpt)
						flavor.eventRecorder.Event(claim, v1.EventTypeNormal, "ProvisionStorage", fmt.Errorf("override of size and sizeInGiB is not permitted, default to claim capacity of %v", valOpt).Error())
						continue
					}
				}
				log.Infof("adding key: %v with override value: %v", override, annotation)
				optionsMap[override] = annotation
			}
		}
	}

	return optionsMap, nil
}

// MetaUIDFunc is an IndexFunc used to cache PVCs by their UID
func MetaUIDFunc(obj interface{}) ([]string, error) {
	object, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("Object cannot be accessed with obj %v: %v", obj, err.Error())
	}

	return []string{string(object.GetUID())}, nil
}

func (flavor *Flavor) getNodeInfoByUUID(uuid string) (*crd_v1.HPENodeInfo, error) {
	log.Tracef(">>>>> getNodeInfoByUUID with uuid %s", uuid)
	defer log.Trace("<<<<< getNodeInfoByUUID")

	nodeInfoList, err := flavor.crdClient.StorageV1().HPENodeInfos().List(meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, nodeInfo := range nodeInfoList.Items {
		if nodeInfo.Spec.UUID == uuid {
			return &nodeInfo, nil
		}
	}
	return nil, nil
}

func (flavor *Flavor) getNodeInfoByName(name string) (*crd_v1.HPENodeInfo, error) {
	log.Tracef(">>>>> getNodeInfoByName with name %s", name)
	defer log.Trace("<<<<< getNodeInfoByName")

	nodeInfoList, err := flavor.crdClient.StorageV1().HPENodeInfos().List(meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, nodeInfo := range nodeInfoList.Items {
		if nodeInfo.Name == name {
			return &nodeInfo, nil
		}
	}

	return nil, nil
}

// GetCredentialsFromSecret retrieves the secrets map for the given secret name and namespace if exists, else returns nil
func (flavor *Flavor) GetCredentialsFromSecret(name string, namespace string) (map[string]string, error) {
	log.Tracef(">>>>> GetCredentialsFromSecret, name: %s, namespace: %s", name, namespace)
	defer log.Trace("<<<<< GetCredentialsFromSecret")

	secret, err := flavor.kubeClient.CoreV1().Secrets(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting secret %s in namespace %s: %v", name, namespace, err)
	}

	credentials := map[string]string{}
	for key, value := range secret.Data {
		credentials[key] = string(value)
	}
	return credentials, nil
}

// IsPodExists checks if the pod with the given uid exists on the cluster
func (flavor *Flavor) IsPodExists(uid string) (bool, error) {
	log.Tracef(">>>>> IsPodExists, id: %s", uid)
	defer log.Trace("<<<<< IsPodExists")

	podList, err := flavor.kubeClient.CoreV1().Pods("").List(meta_v1.ListOptions{})
	if err != nil {
		log.Errorf("Error retrieving the pods, err: %v", err.Error())
		return false, err
	}
	for _, pod := range podList.Items {
		if uid == fmt.Sprintf("%s", pod.UID) {
			return true, nil // Pod Found
		}
	}
	// Pod not found or missing
	log.Tracef("Pod with uid %s not found", uid)
	return false, nil
}

func (flavor *Flavor) getPodByName(name string, namespace string) (*v1.Pod, error) {
	log.Tracef(">>>>> getPodByName, name: %s, namespace: %s", name, namespace)
	defer log.Trace("<<<<< getPodByName")

	pod, err := flavor.kubeClient.CoreV1().Pods(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("Error retrieving the pod %s/%s, err: %v", namespace, name, err.Error())
		return nil, err
	}
	return pod, nil
}

// makeVolumeHandle returns csi-<sha256(podUID,volSourceSpecName)>
// Original source location: kubernetes/pkg/volume/csi/csi_mounter.go
// TODO: Must be in-sync with k8s code
func makeVolumeHandle(podUID, volSourceSpecName string) string {
	result := sha256.Sum256([]byte(fmt.Sprintf("%s%s", podUID, volSourceSpecName)))
	return fmt.Sprintf("csi-%x", result)
}

// GetEphemeralVolumeSecretFromPod retrieves secret for a given CSI volname from a specified POD name and namespace
func (flavor *Flavor) GetEphemeralVolumeSecretFromPod(volumeHandle string, podName string, namespace string) (string, error) {
	log.Tracef(">>>>> GetEphemeralVolumeSecretFromPod, volumeHandle: %s, podName: %s, namespace: %s", volumeHandle, podName, namespace)
	defer log.Trace("<<<<< GetEphemeralVolumeSecretFromPod")

	pod, err := flavor.getPodByName(podName, namespace)
	if err != nil {
		log.Errorf("Unable to get pod %s/%s for volume %s, err: %v", namespace, podName, volumeHandle, err.Error())
		return "", err
	}
	if pod == nil {
		return "", fmt.Errorf("Pod %s/%s not found", namespace, podName)
	}

	for _, vol := range pod.Spec.Volumes {
		// Compute the volumeHandle for each CSI volume and match with the given volumeHandle
		handle := makeVolumeHandle(string(pod.GetUID()), vol.Name)
		if handle == volumeHandle {
			log.Tracef("Matched ephemeral volume %s attached to the POD [%s/%s]", vol.Name, namespace, podName)
			csiSource := vol.VolumeSource.CSI
			if csiSource == nil {
				return "", fmt.Errorf("CSI volume source is nil")
			}

			// No secrets are configured
			if csiSource.NodePublishSecretRef == nil {
				log.Error("No secrets are configured")
				return "", fmt.Errorf("Missing 'NodePublishSecretRef' in the POD spec")
			}
			return csiSource.NodePublishSecretRef.Name, nil
		}
	}
	return "", fmt.Errorf("Pod %s/%s does not contain the volume %s", namespace, podName, volumeHandle)
}

// GetVolumePropertyOfPV retrieves volume filesystem for a given CSI volname
func (flavor *Flavor) GetVolumePropertyOfPV(propertyName string, pvName string) (string, error) {
	log.Tracef(">>>>> GetVolumePropertyOfPV, pvName: %s, propertyName: %s", pvName, propertyName)
	defer log.Trace("<<<<< GetVolumePropertyOfPV")

	pv, err := flavor.kubeClient.CoreV1().PersistentVolumes().Get(pvName, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("Error retrieving the attribtue %s of the pv %s, err: %v", propertyName, pvName, err.Error())
		return "", err
	}
	volAttr := pv.Spec.CSI.VolumeAttributes

	if propertyVal, found := volAttr[propertyName]; found {
		return propertyVal, nil
	}
	return "", nil
}

func (flavor *Flavor) GetOrchestratorVersion() (*version.Info, error) {
	log.Tracef(">>>>> GetOrchestratorVersion")
	defer log.Tracef("<<<<< GetOrchestratorVersion")

	versionInfo, err := flavor.kubeClient.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}
	log.Tracef("obtained k8s version as %s", versionInfo.String())
	return versionInfo, nil
}

func (flavor *Flavor) GetNodeByName(nodeName string) (*v1.Node, error) {
	log.Tracef(">>>>> GetNodeByName called with %s", nodeName)
	defer log.Trace("<<<<< GetNodeByName")

	node, err := flavor.kubeClient.CoreV1().Nodes().Get(nodeName, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("unable to get node with name %s, err %s", nodeName, err.Error())
		return nil, err
	}
	return node, nil
}

func (flavor *Flavor) DeletePod(podName string, namespace string, force bool) error {
	log.Tracef(">>>>> DeletePod called with pod %s in namespace %s", podName, namespace)
	defer log.Trace("<<<<< DeletePod")

	deleteOptions := meta_v1.DeleteOptions{}
	if force {
		gracePeriodSec := int64(0)
		deleteOptions.GracePeriodSeconds = &gracePeriodSec
	}
	err := flavor.kubeClient.CoreV1().Pods(namespace).Delete(podName, &deleteOptions)
	if err != nil {
		return err
	}
	return nil
}

func (flavor *Flavor) ListVolumeAttachments() (*storage_v1.VolumeAttachmentList, error) {
	log.Trace(">>>>> ListVolumeAttachments")
	defer log.Trace("<<<<< ListVolumeAttachments")

	vaList, err := flavor.kubeClient.StorageV1().VolumeAttachments().List(meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return vaList, nil
}

func (flavor *Flavor) DeleteVolumeAttachment(va string, force bool) error {
	log.Trace(">>>>> DeleteVolumeAttachment")
	defer log.Trace("<<<<< DeleteVolumeAttachment")

	deleteOptions := meta_v1.DeleteOptions{}
	if force {
		gracePeriodSec := int64(0)
		deleteOptions.GracePeriodSeconds = &gracePeriodSec
	}
	err := flavor.kubeClient.StorageV1().VolumeAttachments().Delete(va, &deleteOptions)
	if err != nil {
		return err
	}
	return nil
}

// MonitorPod monitors pods with given labels for being in unknown state(node unreachable/lost) and assist them to move to different node
func (flavor *Flavor) MonitorPod(podLabelkey, podLabelvalue string) error {
	log.Tracef(">>>>> MonitorPod with label %s=%s", podLabelkey, podLabelvalue)
	defer log.Tracef("<<<<< MonitorPod")

	labelSelector := meta_v1.LabelSelector{MatchLabels: map[string]string{podLabelkey: podLabelvalue}}
	listOptions := meta_v1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}

	podList, err := flavor.kubeClient.CoreV1().Pods(meta_v1.NamespaceAll).List(listOptions)
	if err != nil {
		log.Errorf("unable to list nfs pods for monitoring: %s", err.Error())
		return err
	}
	if podList == nil || len(podList.Items) == 0 {
		log.Tracef("cannot find any nfs pod with label %s=%s", podLabelkey, podLabelvalue)
		return nil
	}

	for _, pod := range podList.Items {
		podUnknownState := false
		if pod.Status.Reason == nodeLostCondition {
			podUnknownState = true
		} else if pod.ObjectMeta.DeletionTimestamp != nil {
			node, err := flavor.GetNodeByName(pod.Spec.NodeName)
			if err != nil {
				log.Warnf("unable to get node for monitoring pod %s: %s", pod.ObjectMeta.Name, err.Error())
				// move on with other pods
				continue
			}

			// Check if node has eviction taint
			for _, taint := range node.Spec.Taints {
				if taint.Key == nodeUnreachableTaint &&
					taint.Effect == v1.TaintEffectNoExecute {
					podUnknownState = true
					break
				}
			}
		}
		// no further action required if pod is not in unknown state, continue with other pods
		if !podUnknownState {
			continue
		}
		// delete volume attachments if the node is down for this pod
		log.Infof("Force deleting volume attachment for pod %s as it is in unknown state.", pod.ObjectMeta.Name)
		err := flavor.cleanupVolumeAttachmentsByPod(&pod)
		if err != nil {
			log.Errorf("Error cleaning up volume attachments for pod %s: %s", pod.ObjectMeta.Name, err.Error())
			// move on with other pods
			continue
		}

		// force delete the pod
		log.Infof("Force deleting pod %s as it's in unknown state.", pod.ObjectMeta.Name)
		err = flavor.DeletePod(pod.Name, pod.ObjectMeta.Namespace, true)
		if err != nil {
			if !errors.IsNotFound(err) {
				log.Errorf("Error deleting pod %s: %s", pod.Name, err.Error())
			}
		}
	}
	return nil
}

func (flavor *Flavor) cleanupVolumeAttachmentsByPod(pod *v1.Pod) error {
	log.Tracef(">>>>> cleanupVolumeAttachmentsByPod for pod %s", pod.Name)
	defer log.Tracef("<<<<< cleanupVolumeAttachmentsByPod")

	// Get all volume attachments
	vaList, err := flavor.ListVolumeAttachments()
	if err != nil {
		return err
	}

	if len(vaList.Items) > 0 {
		for _, va := range vaList.Items {
			//check if this va refers to PV which is attached to this pod on same node
			isAttached, err := flavor.isVolumeAttachedToPod(pod, &va)
			if err != nil {
				log.Warnf("unable to determine if va %s belongs to pod %s", va.Name, pod.ObjectMeta.Name)
				continue
			}
			if isAttached {
				err := flavor.DeleteVolumeAttachment(va.Name, false)
				if err != nil {
					return err
				}
				log.Infof("Deleted volume attachment: %s", va.Name)
			}
		}
	}
	return nil
}

// check if the volume attachment refers to PV claimed by the pod on same node
func (flavor *Flavor) isVolumeAttachedToPod(pod *v1.Pod, va *storage_v1.VolumeAttachment) (bool, error) {
	log.Tracef(">>>>> isVolumeAttachedToPod with pod %s, va %s", pod.ObjectMeta.Name, va.Name)
	defer log.Tracef("<<<<< isVolumeAttachedToPod")

	// check only va's attached to node where pod belongs to
	if pod.Spec.NodeName != va.Spec.NodeName {
		return false, nil
	}
	for _, vol := range pod.Spec.Volumes {
		if vol.VolumeSource.PersistentVolumeClaim != nil {
			// get claim from volumeattachment
			claim, err := flavor.getClaimFromClaimName(*va.Spec.Source.PersistentVolumeName)
			if err != nil {
				return false, err
			}
			if claim != nil && claim.ObjectMeta.Name == vol.VolumeSource.PersistentVolumeClaim.ClaimName {
				log.Tracef("volume %s of volumeattachment %s is attached to pod %s", *va.Spec.Source.PersistentVolumeName, va.Name, pod.ObjectMeta.Name)
				return true, nil
			}
		}
	}
	return false, nil
}
