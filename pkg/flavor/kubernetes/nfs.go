// Copyright 2019 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/hpe-storage/common-host-libs/logger"
	"golang.org/x/mod/semver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	nfsPrefix           = "hpe-nfs-"
	defaultNFSNamespace = "hpe-nfs"
	defaultNFSImage     = "hpestorage/nfs-provisioner:v1.0.0"

	creationInterval           = 60 // 300s with sleep interval of 5s
	creationDelay              = 5 * time.Second
	defaultExportPath          = "/export"
	nfsResourceLimitsCPUKey    = "nfsResourceLimitsCpuM"
	nfsResourceLimitsMemoryKey = "nfsResourceLimitsMemoryMi"
	nfsMountOptionsKey         = "nfsMountOptions"
	nfsResourceLabelKey        = "nfsResourceLabel"
	nfsNodeSelectorKey         = "csi.hpe.com/hpe-nfs"
	nfsNodeSelectorValue       = "true"
	nfsParentVolumeIDKey       = "nfs-parent-volume-id"
	nfsNamespaceKey            = "nfsNamespace"
	nfsProvisionerImageKey     = "nfsProvisionerImage"
	pvcKind                    = "PersistentVolumeClaim"
	nfsConfigFile              = "ganesha.conf"
	nfsConfigMap               = "hpe-nfs-config"
	nfsServiceAccount          = "hpe-csi-nfs-sa"
	defaultPodLabelKey         = "monitored-by"
	defaultPodLabelValue       = "hpe-csi"
)

// NFSSpec for creating NFS resources
type NFSSpec struct {
	volumeClaim          string
	resourceRequirements *core_v1.ResourceRequirements
	nodeSelector         map[string]string
	image                string
	labelKey             string
	labelValue           string
}

// CreateNFSVolume creates nfs volume abstracting underlying nfs pvc, deployment and service
func (flavor *Flavor) CreateNFSVolume(pvName string, reqVolSize int64, parameters map[string]string, volumeContentSource *csi.VolumeContentSource) (nfsVolume *csi.Volume, rollback bool, err error) {
	log.Tracef(">>>>> CreateNFSVolume with %s", pvName)
	defer log.Tracef("<<<<< CreateNFSVolume")

	nfsResourceNamespace := defaultNFSNamespace

	if namespace, ok := parameters[nfsNamespaceKey]; ok {
		nfsResourceNamespace = namespace
	}
	// create namespace if not already present
	_, err = flavor.getNFSNamespace(nfsResourceNamespace)
	if err != nil {
		_, err = flavor.createNFSNamespace(nfsResourceNamespace)
		if err != nil {
			return nil, false, err
		}
	}

	// obtain NFS resource parameters
	nfsSpec, err := flavor.getNFSSpec(parameters)
	if err != nil {
		return nil, false, err
	}

	// get pvc from request
	claim, err := flavor.getClaimFromClaimName(pvName)
	if err != nil {
		return nil, false, err
	}

	// clone pvc and modify to RWO mode
	claimClone, err := flavor.cloneClaim(claim, nfsResourceNamespace)
	if err != nil {
		return nil, false, err
	}

	// create pvc
	newClaim, err := flavor.createNFSPVC(claimClone, nfsResourceNamespace)
	if err != nil {
		flavor.eventRecorder.Event(claim, core_v1.EventTypeWarning, "ProvisionStorage", err.Error())
		return nil, true, err
	}

	// update newly created nfs claim in nfs spec
	nfsSpec.volumeClaim = newClaim.ObjectMeta.Name

	// create nfs service account in the namespace
	err = flavor.createServiceAccount(nfsResourceNamespace)
	if err != nil {
		flavor.eventRecorder.Event(claim, core_v1.EventTypeWarning, "ProvisionStorage", err.Error())
		return nil, true, err
	}

	nfsHostDomain, err := flavor.getNFSHostDomain()
	if err != nil {
		return nil, true, err
	}
	// create nfs configmap
	err = flavor.createNFSConfigMap(nfsResourceNamespace, nfsHostDomain)
	if err != nil {
		flavor.eventRecorder.Event(claim, core_v1.EventTypeWarning, "ProvisionStorage", err.Error())
		return nil, true, err
	}

	// create deployment with name hpe-nfs-<originalclaim-uid>
	deploymentName := fmt.Sprintf("%s%s", nfsPrefix, claim.ObjectMeta.UID)
	err = flavor.createNFSDeployment(deploymentName, nfsSpec, nfsResourceNamespace)
	if err != nil {
		flavor.eventRecorder.Event(claim, core_v1.EventTypeWarning, "ProvisionStorage", err.Error())
		return nil, true, err
	}

	// create service with name hpe-nfs-svc-<originalclaim-uid>
	serviceName := fmt.Sprintf("%s%s", nfsPrefix, claim.ObjectMeta.UID)
	err = flavor.createNFSService(serviceName, nfsResourceNamespace)
	if err != nil {
		flavor.eventRecorder.Event(claim, core_v1.EventTypeWarning, "ProvisionStorage", err.Error())
		return nil, true, err
	}

	// get underlying NFS volume properties and copy onto original volume
	volumeContext := make(map[string]string)
	pv, err := flavor.getPvFromName(fmt.Sprintf("pvc-%s", newClaim.ObjectMeta.UID))
	if err != nil {
		return nil, true, err
	}

	if pv.Spec.PersistentVolumeSource.CSI != nil {
		for k, v := range pv.Spec.PersistentVolumeSource.CSI.VolumeAttributes {
			// ignore any annotations added to underlying NFS claim
			if k != "nfsPVC" {
				volumeContext[k] = v
			}
		}
	}

	// decorate NFS PV with its volume handle as label for easy lookup during RWX PV deletion
	pv.ObjectMeta.Labels = make(map[string]string)
	pv.ObjectMeta.Labels[nfsParentVolumeIDKey] = fmt.Sprintf("%s", claim.ObjectMeta.UID)
	flavor.kubeClient.CoreV1().PersistentVolumes().Update(pv)

	// Return newly created underlying nfs claim uid with pv attributes
	return &csi.Volume{
		VolumeId:      fmt.Sprintf("%s", claim.ObjectMeta.UID),
		CapacityBytes: reqVolSize,
		VolumeContext: volumeContext,
		ContentSource: volumeContentSource,
	}, false, nil
}

func (flavor *Flavor) createServiceAccount(nfsNamespace string) error {
	log.Tracef(">>>>> createServiceAccount with namespace %s", nfsNamespace)
	defer log.Tracef("<<<<< createServiceAccount")

	_, err := flavor.kubeClient.CoreV1().ServiceAccounts(nfsNamespace).Create(&core_v1.ServiceAccount{ObjectMeta: meta_v1.ObjectMeta{Name: nfsServiceAccount}})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

func (flavor *Flavor) createNFSConfigMap(nfsNamespace, hostDomain string) error {
	log.Tracef(">>>>> createNFSConfigMap with namespace %s, domain %s", nfsNamespace, hostDomain)
	defer log.Tracef("<<<<< createNFSConfigMap")

	nfsGaneshaConfig := `
NFS_Core_Param
{
  NFS_Protocols= 4;
  NFS_Port = 2049;
  fsid_device = false;
}
NFSv4
{
  Graceless = true;
  UseGetpwnam = true;
  DomainName = REPLACE_DOMAIN;
}
EXPORT
{
  Export_Id = 716;
  Path = /export;
  Pseudo = /export;
  Access_Type = RW;
  Squash = No_Root_Squash;
  Transports = TCP;
  Protocols = 4;
  SecType = "sys";
  FSAL {
      Name = VFS;
  }
}`

	nfsGaneshaConfig = strings.Replace(nfsGaneshaConfig, "REPLACE_DOMAIN", hostDomain, 1)

	configMap := &core_v1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      nfsConfigMap,
			Namespace: nfsNamespace,
			Labels:    createNFSAppLabels(),
		},
		Data: map[string]string{
			nfsConfigFile: nfsGaneshaConfig,
		},
	}
	_, err := flavor.kubeClient.CoreV1().ConfigMaps(nfsNamespace).Create(configMap)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}

	log.Debugf("configmap %s successfully created in namespace %s", nfsConfigMap, nfsNamespace)
	return nil
}

func (flavor *Flavor) RollbackNFSResources(nfsResourceName string, nfsNamespace string) error {
	log.Tracef(">>>>> RollbackNFSResources with name %s namespace %s", nfsResourceName, nfsNamespace)
	defer log.Tracef("<<<<< RollbackNFSResources")
	err := flavor.deleteNFSResources(nfsResourceName, nfsNamespace)
	if err != nil {
		return err
	}
	return nil
}

// DeleteNFSVolume deletes nfs volume which represents nfs pvc, deployment and service
func (flavor *Flavor) DeleteNFSVolume(volumeID string) error {
	log.Tracef(">>>>> DeleteNFSVolume with %s", volumeID)
	defer log.Tracef("<<<<< DeleteNFSVolume")

	nfsResourceName, err := flavor.getNFSResourceNameByVolumeID(volumeID)
	if err != nil {
		return err
	}
	nfsNamespace, err := flavor.getNFSNamespaceByVolumeID(volumeID)
	if err != nil {
		return err
	}
	err = flavor.deleteNFSResources(nfsResourceName, nfsNamespace)
	if err != nil {
		return err
	}

	return err
}

func (flavor *Flavor) deleteNFSResources(nfsResourceName, nfsNamespace string) (err error) {
	// delete deployment deployment/hpe-nfs-<originalclaim-uid>
	err = flavor.deleteNFSDeployment(nfsResourceName, nfsNamespace)
	if err != nil {
		log.Errorf("unable to delete nfs deployment %s as part of cleanup, err %s", nfsResourceName, err.Error())
	}

	// delete nfs pvc pvc/hpe-nfs-<originalclaim-uuid>
	// if deployment is still around, then pvc cannot be deleted due to protection, try to cleanup as best effort
	err = flavor.deleteNFSPVC(nfsResourceName, nfsNamespace)
	if err != nil {
		log.Errorf("unable to delete nfs pvc %s as part of cleanup, err %s", nfsResourceName, err.Error())
	}

	// delete service service/hpe-nfs-<originalclaim-uid>
	err = flavor.deleteNFSService(nfsResourceName, nfsNamespace)
	if err != nil {
		log.Errorf("unable to delete nfs service %s as part of cleanup, err %s", nfsResourceName, err.Error())
	}
	return err
}

func (flavor *Flavor) getNFSResourceNameByVolumeID(volumeID string) (string, error) {
	// get underlying by NFS(RWX) PV volume-id
	pv, err := flavor.getPVByNFSLabel(nfsParentVolumeIDKey, volumeID)
	if err != nil {
		return "", fmt.Errorf("unable to obtain nfs resource name from volume-id %s, err %s", volumeID, err.Error())
	}
	if pv == nil {
		return "", nil
	}
	// get NFS claim name from pv of format hpe-nfs-<rwx-pvc-uid> as generic resource name
	return pv.Spec.ClaimRef.Name, nil
}

func (flavor *Flavor) getNFSNamespaceByVolumeID(volumeID string) (string, error) {
	// get underlying by NFS(RWX) PV volume-id
	pv, err := flavor.getPVByNFSLabel(nfsParentVolumeIDKey, volumeID)
	if err != nil {
		return "", fmt.Errorf("unable to obtain nfs namespace from volume-id %s, err %s", volumeID, err.Error())
	}
	if pv == nil {
		return "", nil
	}
	// return namespace of the corresponding claim for this pv
	return pv.Spec.ClaimRef.Namespace, nil
}

// getMountOptionsFromVolCap returns the mount options from the VolumeCapability if any
func getNFSMountOptions(volumeContext map[string]string) (mountOptions []string) {
	if val, ok := volumeContext[nfsMountOptionsKey]; ok {
		return strings.Split(val, ",")
	}
	return nil
}

func (flavor *Flavor) HandleNFSNodePublish(req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Tracef(">>>>> HandleNFSNodePublish with volume %s target path %s", req.VolumeId, req.TargetPath)
	defer log.Tracef("<<<<< HandleNFSNodePublish")

	var mountOptions []string
	// get nfs claim for corresponding nfs pv
	nfsResourceName, err := flavor.getNFSResourceNameByVolumeID(req.VolumeId)
	if err != nil {
		return nil, err
	}

	nfsNamespace, err := flavor.getNFSNamespaceByVolumeID(req.VolumeId)
	if err != nil {
		return nil, err
	}

	// get service with matching volume-id(i.e original claim-id)
	service, err := flavor.getNFSService(nfsResourceName, nfsNamespace)
	if err != nil {
		log.Errorf("unable to obtain service %s volume-id %s to publish volume", nfsResourceName, req.VolumeId)
		return nil, err
	}
	clusterIP := service.Spec.ClusterIP

	source := fmt.Sprintf("%s:%s", clusterIP, defaultExportPath)
	target := req.GetTargetPath()
	log.Debugf("mounting nfs volume %s to %s", source, target)
	mountOptions = getNFSMountOptions(req.VolumeContext)
	if len(mountOptions) == 0 {
		// use default mount options, i.e (rw,relatime,vers=4.0,rsize=1048576,wsize=1048576,namlen=255,hard,proto=tcp,timeo=600,retrans=2,sec=sys,local_lock=none)
		mountOptions = []string{
			"nolock",
			"vers=4",
		}
	}
	mountOptions = append(mountOptions, fmt.Sprintf("addr=%s", clusterIP))
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	log.Debugf("creating target path %s for nfs mount", target)
	if err := os.MkdirAll(target, 0750); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := flavor.chapiDriver.MountNFSVolume(source, target, mountOptions, "nfs4"); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

// IsNFSVolume returns true if given volumeID belongs to nfs access volume
func (flavor *Flavor) IsNFSVolume(volumeID string) bool {
	// NFS(RWX) pv, will have its volume-id added in underlying PV label
	pv, err := flavor.getPVByNFSLabel(nfsParentVolumeIDKey, volumeID)
	if err != nil {
		log.Tracef("unable to obtain pv based on volume-id %s, err %s", volumeID, err.Error())
		return false
	}
	if pv == nil {
		return false
	}
	return true
}

// GetNFSVolumeID returns underlying volume-id of RWO PV based on RWX PV volume-id, if one exists
func (flavor *Flavor) GetNFSVolumeID(volumeID string) (string, error) {
	log.Tracef(">>>>> GetNFSVolumeID with %s", volumeID)
	defer log.Tracef("<<<<< GetNFSVolumeID")

	// NFS(RWX) pv, will have its volume-id added in underlying PV label
	pv, err := flavor.getPVByNFSLabel(nfsParentVolumeIDKey, volumeID)
	if err != nil {
		log.Tracef("unable to obtain pv based on volume-id %s, err %s", volumeID, err.Error())
		return "", err
	}
	if pv == nil {
		return "", nil
	}
	return pv.Spec.PersistentVolumeSource.CSI.VolumeHandle, nil
}

func (flavor *Flavor) getNFSHostDomain() (string, error) {
	log.Tracef(">>>>> getNFSHostDomain")
	defer log.Tracef("<<<<< getNFSHostDomain")

	// obtain an array of  {hostname, domainname}
	hostNameAndDomain, err := flavor.chapiDriver.GetHostNameAndDomain()
	if err != nil {
		return "", fmt.Errorf("Failed to obtain host name and domain to provision NFS volume, %s", err.Error())
	}
	if len(hostNameAndDomain) != 2 || hostNameAndDomain[1] == "unknown" {
		return "", fmt.Errorf("Unable to obtain valid host domain name to provision NFS volume")
	}
	log.Tracef("Host domain name obtained as %s", hostNameAndDomain[1])
	return strings.TrimSuffix(hostNameAndDomain[1], "."), nil
}

func (flavor *Flavor) getNFSSpec(scParams map[string]string) (*NFSSpec, error) {
	log.Tracef(">>>>> getNFSSpec with %v", scParams)
	defer log.Tracef("<<<<< getNFSSpec")

	var nfsSpec NFSSpec
	resourceLimits := make(core_v1.ResourceList)
	var err error

	// nfs cpu limits eg: cpu=500m
	if val, ok := scParams[nfsResourceLimitsCPUKey]; ok {
		quantity, err := resource.ParseQuantity(val)
		if err != nil {
			return nil, fmt.Errorf("invalid nfs cpu resource limit %s provided in storage class, err %s", val, err.Error())
		}
		resourceLimits[core_v1.ResourceCPU] = quantity
	}

	// nfs memory limits eg: memory=64Mi
	if val, ok := scParams[nfsResourceLimitsMemoryKey]; ok {
		quantity, err := resource.ParseQuantity(val)
		if err != nil {
			return nil, fmt.Errorf("invalid nfs memory resource limit %s provided in storage class, err %s", val, err.Error())
		}
		resourceLimits[core_v1.ResourceMemory] = quantity
	}

	if len(resourceLimits) != 0 {
		nfsSpec.resourceRequirements = &core_v1.ResourceRequirements{Limits: resourceLimits}
	}

	// get nodes with hpe-nfs labels
	nodes, err := flavor.getNFSNodes()
	if err != nil {
		return nil, err
	}

	// use node-selector for deployment if we find nodes with hpe-nfs label
	if len(nodes) > 0 {
		nfsSpec.nodeSelector = map[string]string{nfsNodeSelectorKey: nfsNodeSelectorValue}
	}

	// use nfs provisioner image specified in storage class
	nfsSpec.image = defaultNFSImage
	if image, ok := scParams[nfsProvisionerImageKey]; ok {
		nfsSpec.image = image
	}

	// initialize with default label
	nfsSpec.labelKey = defaultPodLabelKey
	nfsSpec.labelValue = defaultPodLabelValue
	// apply override if provided by user
	if label, ok := scParams[nfsResourceLabelKey]; ok {
		items := strings.Split(label, "=")
		if len(items) == 2 {
			nfsSpec.labelKey = strings.TrimSpace(items[0])
			nfsSpec.labelValue = strings.TrimSpace(items[1])
		}
	}
	return &nfsSpec, nil
}

func (flavor *Flavor) getPVByNFSLabel(name string, value string) (*core_v1.PersistentVolume, error) {
	log.Tracef(">>>>> getPVByNFSLabel with key %s value %s", name, value)
	defer log.Tracef("<<<<< getPVByNFSLabel")

	labelSelector := meta_v1.LabelSelector{MatchLabels: map[string]string{name: value}}
	listOptions := meta_v1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}

	pvList, err := flavor.kubeClient.CoreV1().PersistentVolumes().List(listOptions)
	if err != nil {
		return nil, err
	}
	if pvList == nil || len(pvList.Items) == 0 {
		// no PV's found with label
		return nil, nil
	}
	if len(pvList.Items) > 1 {
		// this should never happen
		return nil, fmt.Errorf("multiple pv's found with same nfs label %s=%s", name, value)
	}
	return &pvList.Items[0], nil
}

func (flavor *Flavor) getPvFromName(pvName string) (*core_v1.PersistentVolume, error) {
	log.Tracef(">>>>> getPvFromName with claim %s", pvName)
	defer log.Tracef("<<<<< getPvFromName")

	pv, err := flavor.kubeClient.CoreV1().PersistentVolumes().Get(pvName, meta_v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return pv, nil
}

func (flavor *Flavor) cloneClaim(claim *core_v1.PersistentVolumeClaim, nfsNamespace string) (*core_v1.PersistentVolumeClaim, error) {
	log.Tracef(">>>>> cloneClaim with claim %s", claim.ObjectMeta.Name)
	defer log.Tracef("<<<<< cloneClaim")

	// get copy of original pvc
	claimClone := claim.DeepCopy()
	claimClone.ObjectMeta.Namespace = nfsNamespace
	claimClone.ObjectMeta.ResourceVersion = ""
	// modify name with hpe-nfs-pvc-<originalclaim-uid>
	claimClone.ObjectMeta.Name = fmt.Sprintf("%s%s", nfsPrefix, claim.ObjectMeta.UID)
	// change access-mode to RWO for underlying nfs pvc
	claimClone.Spec.AccessModes = []core_v1.PersistentVolumeAccessMode{core_v1.ReadWriteOnce}
	// add annotation indicating this is an underlying nfs volume
	claimClone.ObjectMeta.Annotations["csi.hpe.com/nfsPVC"] = "true"

	// if clone is requested from existing pvc, ensure child-claim(i.e RWO type) is used instead
	if claim.Spec.DataSource != nil && claim.Spec.DataSource.Kind == pvcKind {
		// fetch source claim
		sourceClaim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Get(claim.Spec.DataSource.Name, meta_v1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot fetch source claim %s for requested clone, err %s", claim.Spec.DataSource.Name, err.Error())
		}
		// check if a PVC exists with name hpe-nfs-<original-claim-uid> and replace that as data-source.
		childClaim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Get(fmt.Sprintf("%s%s", nfsPrefix, sourceClaim.ObjectMeta.UID), meta_v1.GetOptions{})
		if err == nil && childClaim != nil {
			log.Tracef("replacing datasource from %s to %s for nfs claim %s creation", claim.Spec.DataSource.Name, childClaim.ObjectMeta.Name, claim.ObjectMeta.Name)
			claimClone.Spec.DataSource.Name = childClaim.ObjectMeta.Name
		}
	}
	return claimClone, nil
}

// createNFSPVC creates Kubernetes Persistent Volume Claim
func (flavor *Flavor) createNFSPVC(claim *core_v1.PersistentVolumeClaim, nfsNamespace string) (*core_v1.PersistentVolumeClaim, error) {
	log.Tracef(">>>>> createNFSPVC with claim %s", claim.ObjectMeta.Name)
	defer log.Tracef("<<<<< createNFSPVC")

	// create new underlying nfs claim
	newClaim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Create(claim)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return nil, err
		}
		// claim already exists, get details
		newClaim, err = flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Get(claim.ObjectMeta.Name, meta_v1.GetOptions{})
		if err != nil {
			return nil, err
		}
	}

	// wait for pvc to be bound
	err = flavor.waitForPVCCreation(newClaim.ObjectMeta.Name, nfsNamespace)
	if err != nil {
		return nil, err
	}

	log.Infof("PVC %s is in bound state", newClaim.ObjectMeta.Name)
	return newClaim, nil
}

// createNFSService creates a NFS service with given name
func (flavor *Flavor) createNFSService(svcName string, nfsNamespace string) error {
	log.Tracef(">>>>> createNFSService with service name %s", svcName)
	defer log.Tracef("<<<<< createNFSService")

	// validate nfs service settings
	if svcName == "" {
		return fmt.Errorf("empty service name provided for creating nfs nfs service")
	}

	// check if nfs service already exists
	getAction := func() error {
		_, err := flavor.kubeClient.CoreV1().Services(nfsNamespace).Get(svcName, meta_v1.GetOptions{})
		return err
	}
	exists, err := flavor.resourceExists(getAction, "service", svcName)
	if err == nil && exists {
		log.Infof("nfs service %s exists in %s namespace", svcName, nfsNamespace)
		return nil
	}

	// create the nfs service
	service := flavor.makeNFSService(svcName, nfsNamespace)
	if _, err := flavor.kubeClient.CoreV1().Services(nfsNamespace).Create(service); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create nfs service %s, err %+v", svcName, err)
		}
		log.Infof("nfs service %s already exists", svcName)
	} else {
		log.Infof("nfs service %s started", svcName)
	}
	return nil
}

func (flavor *Flavor) getNFSNamespace(namespace string) (*core_v1.Namespace, error) {
	log.Tracef(">>>>> getNFSNamespace with namespace name %s", namespace)
	defer log.Tracef("<<<<< getNFSNamespace")

	ns, err := flavor.kubeClient.CoreV1().Namespaces().Get(namespace, meta_v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return ns, nil
}

func (flavor *Flavor) createNFSNamespace(namespace string) (*core_v1.Namespace, error) {
	log.Tracef(">>>>> createNFSNamespace with namespace name %s", namespace)
	defer log.Tracef("<<<<< createNFSNamespace")

	spec := &core_v1.Namespace{ObjectMeta: meta_v1.ObjectMeta{Name: namespace}}
	ns, err := flavor.kubeClient.CoreV1().Namespaces().Create(spec)
	if err != nil {
		return nil, err
	}
	return ns, nil
}

func (flavor *Flavor) getNFSService(svcName, nfsNamespace string) (*core_v1.Service, error) {
	log.Tracef(">>>>> getNFSService with service name %s", svcName)
	defer log.Tracef("<<<<< getNFSService")

	service, err := flavor.kubeClient.CoreV1().Services(nfsNamespace).Get(svcName, meta_v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

// getNFSNodes returns nodes labeled to run HPE NFS Pods
func (flavor *Flavor) getNFSNodes() ([]core_v1.Node, error) {
	log.Tracef(">>>>> getNFSNodes")
	defer log.Tracef("<<<<< getNFSNodes")

	// check if nfs service already exists
	nodeList, err := flavor.kubeClient.CoreV1().Nodes().List(meta_v1.ListOptions{LabelSelector: strings.Join([]string{nfsNodeSelectorKey, nfsNodeSelectorValue}, "=")})
	if err != nil {
		log.Errorf("unable to get list of nodes with hpe-nfs label, err %s", err.Error())
		return nil, err
	}
	return nodeList.Items, nil
}

func createNFSAppLabels() map[string]string {
	return map[string]string{
		"app": "hpe-nfs",
	}
}

// CreateNFSDeployment creates a nfs deployment with given name
func (flavor *Flavor) createNFSDeployment(deploymentName string, nfsSpec *NFSSpec, nfsNamespace string) error {
	log.Tracef(">>>>> createNFSDeployment with name %s volume %s", deploymentName, nfsSpec.volumeClaim)
	defer log.Tracef("<<<<< createNFSDeployment")

	// validate nfs deployment settings
	if deploymentName == "" {
		return fmt.Errorf("empty name provided for creating NFS nfs deployment")
	}

	// create a nfs deployment
	deployment := flavor.makeNFSDeployment(deploymentName, nfsSpec, nfsNamespace)
	if _, err := flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Create(deployment); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create nfs deployment %s, err %+v", deploymentName, err)
		}
		log.Infof("nfs deployment %s already exists", deploymentName)
	}
	// make sure its available
	err := flavor.waitForDeployment(deploymentName, nfsNamespace)
	if err != nil {
		return err
	}
	// successfully created nfs deployment. make sure its running.
	log.Infof("nfs deployment %s started", deploymentName)

	return nil
}

//nolint
func (flavor *Flavor) makeNFSService(svcName string, nfsNamespace string) *core_v1.Service {
	log.Tracef(">>>>> makeNFSService with name %s", svcName)
	defer log.Tracef("<<<<< makeNFSService")

	// pod label is of same format as service name, hpe-nfs-<original claim uuid>
	labels := map[string]string{"app": svcName}
	svc := &core_v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      svcName,
			Namespace: nfsNamespace,
			Labels:    labels,
		},
		Spec: core_v1.ServiceSpec{
			Selector: labels,
			Type:     core_v1.ServiceTypeClusterIP,
			Ports: []core_v1.ServicePort{
				{Name: "grpc", Port: 49000, Protocol: core_v1.ProtocolTCP},
				{Name: "nfs-tcp", Port: 2049, Protocol: core_v1.ProtocolTCP},
				{Name: "nfs-udp", Port: 2049, Protocol: core_v1.ProtocolUDP},
				{Name: "nlockmgr-tcp", Port: 32803, Protocol: core_v1.ProtocolTCP},
				{Name: "nlockmgr-udp", Port: 32803, Protocol: core_v1.ProtocolUDP},
				{Name: "mountd-tcp", Port: 20048, Protocol: core_v1.ProtocolTCP},
				{Name: "mountd-udp", Port: 20048, Protocol: core_v1.ProtocolUDP},
				{Name: "portmapper-tcp", Port: 111, Protocol: core_v1.ProtocolTCP},
				{Name: "portmapper-udp", Port: 111, Protocol: core_v1.ProtocolUDP},
				{Name: "statd-tcp", Port: 662, Protocol: core_v1.ProtocolTCP},
				{Name: "statd-udp", Port: 662, Protocol: core_v1.ProtocolUDP},
				{Name: "rquotad-tcp", Port: 875, Protocol: core_v1.ProtocolTCP},
				{Name: "rquotad-udp", Port: 875, Protocol: core_v1.ProtocolUDP},
			},
		},
	}
	return svc
}

func (flavor *Flavor) makeNFSDeployment(name string, nfsSpec *NFSSpec, nfsNamespace string) *apps_v1.Deployment {
	log.Tracef(">>>>> makeNFSDeployment with name %s, pvc %s", name, nfsSpec.volumeClaim)
	defer log.Tracef("<<<<< makeNFSDeployment")

	volumes := []core_v1.Volume{}
	// add PVC
	volumes = append(volumes, core_v1.Volume{
		Name: nfsSpec.volumeClaim,
		VolumeSource: core_v1.VolumeSource{
			PersistentVolumeClaim: &core_v1.PersistentVolumeClaimVolumeSource{
				ClaimName: nfsSpec.volumeClaim,
			},
		},
	})

	// add configmap for ganesha.conf
	configMapSrc := &core_v1.ConfigMapVolumeSource{
		Items: []core_v1.KeyToPath{
			{
				Key:  nfsConfigFile,
				Path: nfsConfigFile,
			},
		},
	}
	configMapSrc.Name = nfsConfigMap
	configMapVol := core_v1.Volume{
		Name: nfsConfigMap,
		VolumeSource: core_v1.VolumeSource{
			ConfigMap: configMapSrc,
		},
	}

	volumes = append(volumes, configMapVol)

	podSpec := core_v1.PodTemplateSpec{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{"app": name, nfsSpec.labelKey: nfsSpec.labelValue},
			Annotations: map[string]string{"tags": name},
		},
		Spec: core_v1.PodSpec{
			ServiceAccountName: nfsServiceAccount,
			Containers:         []core_v1.Container{flavor.makeContainer("hpe-nfs", nfsSpec)},
			RestartPolicy:      core_v1.RestartPolicyAlways,
			Volumes:            volumes,
			HostIPC:            false,
			HostNetwork:        false,
		},
	}

	// apply if any node selector is specified by user
	if len(nfsSpec.nodeSelector) != 0 {
		podSpec.Spec.NodeSelector = nfsSpec.nodeSelector
	}

	// apply pod priority(supported from k8s 1.17 onwards to run critical pods in non kube-system namespace)
	k8sVersion, err := flavor.GetOrchestratorVersion()
	if err != nil {
		log.Warnf("unable to obtain k8s version for adding nfs pod priority, err %s", err.Error())
	}
	if k8sVersion != nil && semver.Compare(k8sVersion.String(), "v1.17.0") >= 0 {
		podSpec.Spec.PriorityClassName = "system-cluster-critical"
	}

	d := &apps_v1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name,
			Namespace: nfsNamespace,
		},
		Spec: apps_v1.DeploymentSpec{
			Selector: &meta_v1.LabelSelector{
				MatchLabels: podSpec.Labels,
			},
			Template: podSpec,
			Replicas: int32toPtr(1),
			Strategy: apps_v1.DeploymentStrategy{Type: apps_v1.RecreateDeploymentStrategyType},
		},
	}
	return d
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

// nolint
func (flavor *Flavor) makeContainer(name string, nfsSpec *NFSSpec) core_v1.Container {
	log.Tracef(">>>>> makeContainer with name %s, volume %s", name, nfsSpec.volumeClaim)
	defer log.Tracef("<<<<< makeContainer")

	privileged := true
	securityContext := &core_v1.SecurityContext{
		Privileged: &privileged,
		Capabilities: &core_v1.Capabilities{
			Add: []core_v1.Capability{"SYS_ADMIN", "DAC_READ_SEARCH"},
		},
	}

	cont := core_v1.Container{
		Name:            name,
		Image:           nfsSpec.image,
		ImagePullPolicy: core_v1.PullIfNotPresent,
		SecurityContext: securityContext,
		Env: []core_v1.EnvVar{
			{
				Name:  "GANESHA_OPTIONS",
				Value: getEnv("GANESHA_OPTIONS", "-N NIV_WARN"),
			},
		},
		Ports: []core_v1.ContainerPort{
			{Name: "grpc", ContainerPort: 49000, Protocol: core_v1.ProtocolTCP},
			{Name: "nfs-tcp", ContainerPort: 2049, Protocol: core_v1.ProtocolTCP},
			{Name: "nfs-udp", ContainerPort: 2049, Protocol: core_v1.ProtocolUDP},
			{Name: "nlockmgr-tcp", ContainerPort: 32803, Protocol: core_v1.ProtocolTCP},
			{Name: "nlockmgr-udp", ContainerPort: 32803, Protocol: core_v1.ProtocolUDP},
			{Name: "mountd-tcp", ContainerPort: 20048, Protocol: core_v1.ProtocolTCP},
			{Name: "mountd-udp", ContainerPort: 20048, Protocol: core_v1.ProtocolUDP},
			{Name: "portmapper-tcp", ContainerPort: 111, Protocol: core_v1.ProtocolTCP},
			{Name: "portmapper-udp", ContainerPort: 111, Protocol: core_v1.ProtocolUDP},
			{Name: "statd-tcp", ContainerPort: 662, Protocol: core_v1.ProtocolTCP},
			{Name: "statd-udp", ContainerPort: 662, Protocol: core_v1.ProtocolUDP},
			{Name: "rquotad-tcp", ContainerPort: 875, Protocol: core_v1.ProtocolTCP},
			{Name: "rquotad-udp", ContainerPort: 875, Protocol: core_v1.ProtocolUDP},
		},
		VolumeMounts: []core_v1.VolumeMount{
			{
				Name:      nfsSpec.volumeClaim,
				MountPath: "/export",
			},
			{
				Name:      nfsConfigMap,
				MountPath: "/etc/ganesha.conf",
				SubPath:   nfsConfigFile,
			},
		},
	}

	// apply if any resource requirements are specified by user
	if nfsSpec.resourceRequirements != nil {
		cont.Resources = *nfsSpec.resourceRequirements
	}

	return cont
}

// deleteNFSService deletes NFS service and its depending artifacts
func (flavor *Flavor) deleteNFSService(svcName string, nfsNamespace string) error {
	log.Tracef(">>>>> deleteNFSService with service %s", svcName)
	defer log.Tracef("<<<<< deleteNFSService")

	log.Infof("Deleting nfs service %s from %s namespace", svcName, nfsNamespace)

	propagation := meta_v1.DeletePropagationBackground
	options := &meta_v1.DeleteOptions{PropagationPolicy: &propagation}

	// Delete the nfs service
	err := flavor.kubeClient.CoreV1().Services(nfsNamespace).Delete(svcName, options)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("failed to delete nfs service %s, err %+v", svcName, err)
		return err
	}

	log.Infof("Triggered deletion of nfs service %s", svcName)
	return nil
}

// deleteNFSDeployment deletes NFS service and its depending artifacts
func (flavor *Flavor) deleteNFSDeployment(name string, nfsNamespace string) error {
	log.Tracef(">>>>> deleteNFSDeployment with %s", name)
	defer log.Tracef("<<<<< deleteNFSDeployment")

	propagation := meta_v1.DeletePropagationBackground
	gracePeriod := int64(5)
	options := &meta_v1.DeleteOptions{PropagationPolicy: &propagation, GracePeriodSeconds: &gracePeriod}

	err := flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Delete(name, options)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("failed to delete nfs deployment %s, err %+v", name, err)
		return err
	}

	log.Infof("Triggered deletion of nfs deployment %s", name)
	return nil
}

// deleteNFSPVC deletes NFS service and its depending artifacts
func (flavor *Flavor) deleteNFSPVC(claimName string, nfsNamespace string) error {
	log.Tracef(">>>>> deleteNFSPVC with %s", claimName)
	defer log.Tracef("<<<<< deleteNFSPVC")

	propagation := meta_v1.DeletePropagationBackground
	options := &meta_v1.DeleteOptions{PropagationPolicy: &propagation}
	// Delete the pvc
	err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Delete(claimName, options)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("failed to delete nfs pvc %s, err %+v", claimName, err)
		return err
	}
	log.Infof("Triggered deletion of PVC %s", claimName)
	return nil
}

func (flavor *Flavor) resourceExists(getAction func() error, resourceType string, resourceName string) (bool, error) {
	log.Tracef(">>>>> resourceExists with %s %s", resourceType, resourceName)
	defer log.Tracef("<<<<< resourceExists")

	err := getAction()
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof("confirmed %s %s does not exist", resourceType, resourceName)
			return false, nil
		}
		return false, fmt.Errorf("failed to get %s %s, err %+v", resourceType, resourceName, err)
	}
	return true, nil
}

func (flavor *Flavor) waitForPVCCreation(claimName, nfsNamespace string) error {
	log.Tracef(">>>>> waitForPVCCreation with %s", claimName)
	defer log.Tracef("<<<<< waitForPVCCreation")

	// wait for the resource to be deleted
	sleepTime := creationDelay
	for i := 0; i < creationInterval; i++ {
		// check for the existence of the resource
		claim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Get(claimName, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pvc %s, err %+v", claimName, err)
		}
		if claim.Status.Phase != core_v1.ClaimBound {
			log.Infof("pvc %s still not bound to pv, current state %s. waiting(try %d)...", claimName, claim.Status.Phase, i+1)
			time.Sleep(sleepTime)
			continue
		} else if !claim.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Warnf("rollback of an existing pvc %s is under progress, returning error", claimName)
			return fmt.Errorf("rollback of an existing pvc %s is under progress", claimName)
		}
		// successfully bound
		return nil
	}
	return fmt.Errorf("gave up waiting for pvc %s to be bound", claimName)
}

func (flavor *Flavor) waitForDeployment(deploymentName string, nfsNamespace string) error {
	log.Tracef(">>>>> waitForDeployment with %s", deploymentName)
	defer log.Tracef("<<<<< waitForDeployment")

	// wait for the resource to be deleted
	sleepTime := creationDelay
	for i := 0; i < creationInterval; i++ {
		// check for the existence of the resource
		deployment, err := flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Get(deploymentName, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment %s, err %+v", deploymentName, err)
		}
		if deployment.Status.AvailableReplicas != 1 {
			log.Infof("deployment %s is still not available. waiting(try %d)...", deploymentName, i+1)
			time.Sleep(sleepTime)
			continue
		}
		// successfully running
		return nil
	}
	return fmt.Errorf("gave up waiting for deployment %s to be available", deploymentName)
}

func int32toPtr(i int32) *int32 {
	return &i
}
