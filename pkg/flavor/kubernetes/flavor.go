// Copyright 2019 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	crd_v1 "github.com/hpe-storage/k8s-custom-resources/pkg/apis/hpestorage/v1"
	crd_client "github.com/hpe-storage/k8s-custom-resources/pkg/client/clientset/versioned"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	core_v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	//"k8s.io/kubernetes/pkg/volume/util"
)

const (
	allowOverrides = "allowOverrides"
	cloneOf        = "cloneOf"
	cloneOfPVC     = "cloneOfPVC"
)

var (
	// resyncPeriod describes how often to get a full resync (0=never)
	resyncPeriod = 5 * time.Minute
)

// Flavor of the CSI driver
type Flavor struct {
	kubeClient *kubernetes.Clientset
	crdClient  *crd_client.Clientset
	nodeName   string

	claimInformer cache.SharedIndexInformer
	claimIndexer  cache.Indexer
	claimStopChan chan struct{}

	eventRecorder record.EventRecorder
}

// NewKubernetesFlavor creates a new k8s flavored CSI driver
func NewKubernetesFlavor(nodeService bool) (*Flavor, error) {
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

	return flavor, nil
}

// ConfigureAnnotations takes the PVC annotations and overrides any parameters in the CSI create volume request
func (flavor *Flavor) ConfigureAnnotations(name string, parameters map[string]string) (map[string]string, error) {
	log.Infof(">>>>> ConfigureAnnotations called with PVC Name %s", name)
	defer log.Infof("<<<<< ConfigureAnnotations")

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
	log.Infof(">>>>> LoadNodeInfo called with node %v", node)
	defer log.Infof("<<<<< LoadNodeInfo")

	nodeInfo, err := flavor.getNodeInfoByUUID(node.UUID)
	if err != nil {
		log.Errorf("Error obtaining node info by uuid - %s\n", err.Error())
		return "", err
	}

	if nodeInfo != nil {
		log.Infof("Node info %s already known to cluster\n", nodeInfo.ObjectMeta.Name)
	} else {
		var iqns []string
		for i := 0; i < len(node.Iqns); i++ {
			iqns = append(iqns, *node.Iqns[i])
		}

		var wwpns []string
		for i := 0; i < len(node.Wwpns); i++ {
			wwpns = append(wwpns, *node.Wwpns[i])
		}

		var networks []string
		for i := 0; i < len(node.Networks); i++ {
			networks = append(networks, *node.Networks[i])
		}

		newNodeInfo := &crd_v1.HPENodeInfo{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: node.Name,
			},
			Spec: crd_v1.HPENodeInfoSpec{
				UUID:     node.UUID,
				IQNs:     iqns,
				Networks: networks,
				WWPNs:    wwpns,
			},
		}

		log.Infof("Adding node with name %s", node.Name)
		nodeInfo, err = flavor.crdClient.StorageV1().HPENodeInfos().Create(newNodeInfo)
		if err != nil {
			log.Infof("Error adding node %v - %s", nodeInfo, err.Error())
			return "", nil
		}

		log.Infof("Successfully added node info for node %v", nodeInfo)
		flavor.nodeName = nodeInfo.ObjectMeta.Name
	}

	return node.UUID, nil
}

// UnloadNodeInfo remove the HPENodeInfo from the list of CRDs
func (flavor *Flavor) UnloadNodeInfo() {
	log.Infof(">>>>>> UnloadNodeInfo with name %s", flavor.nodeName)
	defer log.Infof("<<<<<< UnloadNodeInfo")

	err := flavor.crdClient.StorageV1().HPENodeInfos().Delete(flavor.nodeName, &meta_v1.DeleteOptions{})
	if err != nil {
		log.Errorf("Failed to delete node %s - %s", flavor.nodeName, err.Error())
	}
}

// GetNodeInfo retrieves the Node identified by nodeID from the list of CRDs
func (flavor *Flavor) GetNodeInfo(nodeID string) (*model.Node, error) {
	log.Infof(">>>>>> GetNodeInfo from node ID %s", nodeID)
	defer log.Infof("<<<<<< GetNodeInfo")

	nodeInfoList, err := flavor.crdClient.StorageV1().HPENodeInfos().List(meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	log.Infof("Found the following HPE Node Info objects: %v", nodeInfoList)

	for _, nodeInfo := range nodeInfoList.Items {
		log.Infof("Processing node info %v", nodeInfo)

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
				Name:     nodeInfo.ObjectMeta.Name,
				UUID:     nodeInfo.Spec.UUID,
				Iqns:     iqns,
				Networks: networks,
				Wwpns:    wwpns,
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
	log.Infof(">>>>> getVolumeNameFromClaimName called with PVC Name %s", name)
	defer log.Infof("<<<<< getVolumeNameFromClaimName")

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
	log.Infof(">>>>> getClaimFromClaimName called with %s", name)
	defer log.Infof("<<<<< getClaimFromClaimName")

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
	log.Info(">>>> getClassOverrideOptions")
	defer log.Info("<<<<< getClassOverrideOptions")

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
	log.Infof("resulting override keys :%#v", overridekeys)
	return overridekeys
}

func (flavor *Flavor) getClaimOverrideOptions(claim *v1.PersistentVolumeClaim, overrides []string, optionsMap map[string]string, provisioner string) (map[string]string, error) {
	log.Infof(">>>>> getClaimOverrideOptions for %s", provisioner)
	defer log.Infof("<<<<< getClaimOverrideOptions")

	provisionerName := provisioner
	for _, override := range overrides {
		for key, annotation := range claim.Annotations {
			if strings.HasPrefix(strings.ToLower(key), provisionerName+"/"+strings.ToLower(override)) {
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

	// check to see if there is cloneOfPVC annotation present
	volumeName, err := flavor.getPVFromPVCAnnotation(claim, provisioner)
	if err != nil {
		return nil, err
	}

	// update the options map with the pv name if there is one
	if volumeName != "" {
		optionsMap[cloneOf] = volumeName
	}

	// make sure cloneOfPVC is removed from the options (all cases)
	if _, found := optionsMap[cloneOfPVC]; found {
		delete(optionsMap, cloneOfPVC)
	}
	return optionsMap, nil
}

func (flavor *Flavor) getPVFromPVCAnnotation(claim *v1.PersistentVolumeClaim, provisioner string) (string, error) {
	log.Infof(">>>>> getPVFromPVCAnnotation for %s", provisioner)
	defer log.Infof("<<<<< getPVFromPVCAnnotation")

	// check to see if we have the cloneOfPVC annotation
	pvcToClone, foundClonePVC := claim.Annotations[fmt.Sprintf("%s%s", provisioner, cloneOfPVC)]
	if !foundClonePVC {
		return "", nil
	}

	pvName, err := flavor.getVolumeNameFromClaimName(pvcToClone)
	log.Infof("pvc %s maps to pv %s", pvcToClone, pvName)
	if err != nil {
		return "", fmt.Errorf("unable to retrieve pvc %s : %s", pvcToClone, err.Error())
	}
	if pvName == "" {
		return "", fmt.Errorf("unable to retrieve pvc %s : not found", pvcToClone)
	}
	return pvName, nil
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
	log.Infof(">>>>> getNodeInfoByUUID with uuid %s\n", uuid)
	defer log.Infof("<<<<< getNodeInfoByUUID")

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

// GetCredentialsFromSecret retrieves the secrets map for the given secret name and namespace if exists, else returns nil
func (flavor *Flavor) GetCredentialsFromSecret(name string, namespace string) (map[string]string, error) {
	log.Infof(">>>>> GetCredentialsFromSecret, name: %s, namespace: %s", name, namespace)
	defer log.Infof("<<<<< GetCredentialsFromSecret")

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

func (flavor *Flavor) getPodByName(name string, namespace string) (*v1.Pod, error) {
	log.Infof(">>>>> getPodByName, name: %s, namespace: %s", name, namespace)
	defer log.Infof("<<<<< getPodByName")

	pod, err := flavor.kubeClient.CoreV1().Pods(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("Error retrieving the pod %s/%s, err: %v", namespace, name, err.Error())
		return nil, err
	}
	return pod, nil
}

// getCredentialsFromPod retrieves the secrets map from the Pod for the given secret name if exists, else returns nil
func (flavor *Flavor) getCredentialsFromPod(pod *v1.Pod, secretName string) (map[string]string, error) {
	log.Infof(">>>>> getCredentialsFromPod, secretName: %s, podNamespace: %s", secretName, pod.Namespace)
	defer log.Infof("<<<<< getCredentialsFromPod")

	secret := make(map[string]string)
	secrets, err := flavor.kubeClient.CoreV1().Secrets(pod.Namespace).Get(secretName, meta_v1.GetOptions{})
	if err != nil {
		return secret, err
	}
	for name, data := range secrets.Data {
		secret[name] = string(data)
	}
	return secret, nil
}

// makeVolumeHandle returns csi-<sha256(podUID,volSourceSpecName)>
// Original source location: kubernetes/pkg/volume/csi/csi_mounter.go
// TODO: Must be in-sync with k8s code
func makeVolumeHandle(podUID, volSourceSpecName string) string {
	result := sha256.Sum256([]byte(fmt.Sprintf("%s%s", podUID, volSourceSpecName)))
	return fmt.Sprintf("csi-%x", result)
}

// GetCredentialsFromPodSpec retrieves volume secrets for a given CSI volname from a specified POD name and namespace
func (flavor *Flavor) GetCredentialsFromPodSpec(volumeHandle string, podName string, namespace string) (map[string]string, error) {
	log.Tracef(">>>>> GetCredentialsFromPodSpec, volumeHandle: %s, podName: %s, namespace: %s", volumeHandle, podName, namespace)
	defer log.Trace("<<<<< GetCredentialsFromPodSpec")

	pod, err := flavor.getPodByName(podName, namespace)
	if err != nil {
		log.Errorf("Unable to get secrets for volume %s, err: %v", volumeHandle, err.Error())
		return nil, err
	}
	if pod == nil {
		return nil, fmt.Errorf("Pod %s is nil", podName)
	}

	for _, vol := range pod.Spec.Volumes {
		// Compute the volumeHandle for each CSI volume and match with the given volumeHandle
		handle := makeVolumeHandle(string(pod.GetUID()), vol.Name)
		if handle == volumeHandle {
			log.Infof("Matched ephemeral volume %s attached to the POD [%s/%s]", vol.Name, namespace, podName)
			csiSource := vol.VolumeSource.CSI
			if csiSource == nil {
				return nil, fmt.Errorf("CSI volume source is nil")
			}

			// No secrets are configured
			if csiSource.NodePublishSecretRef == nil {
				log.Trace("No secrets are configured")
				return nil, fmt.Errorf("Missing 'NodePublishSecretRef' in the POD spec")
			}

			// Get the secrets from Pod
			secret, err := flavor.getCredentialsFromPod(pod, csiSource.NodePublishSecretRef.Name)
			if err != nil {
				log.Errorf("failed to get secret from [%q/%q]", pod.Namespace, csiSource.NodePublishSecretRef.Name)
				return nil, fmt.Errorf("failed to get secret from [%q/%q]", pod.Namespace, csiSource.NodePublishSecretRef.Name)
			}
			return secret, nil
		}
	}
	return nil, fmt.Errorf("Pod %s/%s does not contain the volume %s", namespace, podName, volumeHandle)
}
