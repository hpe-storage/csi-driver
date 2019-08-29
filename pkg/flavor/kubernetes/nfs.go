// Copyright 2019 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/hpe-storage/common-host-libs/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	nfsPrefix    = "hpe-nfs-"
	nfsNamespace = "hpe-nfs"
	nfsImage     = "nimblestorage/nfs-ganesha"

	deletionInterval           = 30 // 60s with sleep interval of 2s
	deletionDelay              = 2 * time.Second
	creationInterval           = 60 // 120s with sleep interval of 2s
	creationDelay              = 2 * time.Second
	defaultExportPath          = "/export"
	nfsResourceLimitsCPUKey    = "nfsResourceLimitsCpuM"
	nfsResourceLimitsMemoryKey = "nfsResourceLimitsMemoryMi"
	nfsNodeSelectorKey         = "csi.hpe.com/hpe-nfs"
	nfsNodeSelectorValue       = "true"
)

// NFSSpec for creating NFS resources
type NFSSpec struct {
	volumeClaim          string
	resourceRequirements *core_v1.ResourceRequirements
	nodeSelector         map[string]string
}

// CreateNFSVolume creates nfs volume abstracting underlying nfs pvc, deployment and service
func (flavor *Flavor) CreateNFSVolume(pvName string, reqVolSize int64, parameters map[string]string) (nfsVolume *csi.Volume, rollback bool, err error) {
	log.Tracef(">>>>> CreateNFSVolume with %s", pvName)
	defer log.Tracef("<<<<< CreateNFSVolume")

	// create namespace if not already present
	_, err = flavor.GetNFSNamespace(nfsNamespace)
	if err != nil {
		_, err = flavor.CreateNFSNamespace(nfsNamespace)
		if err != nil {
			return nil, false, err
		}
	}

	// obtain NFS resource parameters
	nfsSpec, err := flavor.GetNFSSpec(parameters)
	if err != nil {
		return nil, false, err
	}

	// get pvc from request
	claim, err := flavor.getClaimFromClaimName(pvName)
	if err != nil {
		return nil, false, err
	}

	// clone pvc and modify to RWO mode
	claimClone, err := flavor.cloneClaim(claim)
	if err != nil {
		return nil, false, err
	}

	// create pvc
	newClaim, err := flavor.CreateNFSPVC(claimClone)
	if err != nil {
		flavor.eventRecorder.Event(claim, core_v1.EventTypeWarning, "ProvisionStorage", err.Error())
		return nil, true, err
	}

	// update newly created nfs claim in nfs spec
	nfsSpec.volumeClaim = newClaim.ObjectMeta.Name

	// create deployment with name hpe-nfs-<originalclaim-uid>
	deploymentName := fmt.Sprintf("%s%s", nfsPrefix, claim.ObjectMeta.UID)
	err = flavor.CreateNFSDeployment(deploymentName, nfsSpec)
	if err != nil {
		flavor.eventRecorder.Event(claim, core_v1.EventTypeWarning, "ProvisionStorage", err.Error())
		return nil, true, err
	}

	// create service with name hpe-nfs-svc-<originalclaim-uid>
	serviceName := fmt.Sprintf("%s%s", nfsPrefix, claim.ObjectMeta.UID)
	err = flavor.CreateNFSService(serviceName)
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

	// Return newly created underlying nfs claim uid with pv attributes
	return &csi.Volume{
		VolumeId:      fmt.Sprintf("%s", claim.ObjectMeta.UID),
		CapacityBytes: reqVolSize,
		VolumeContext: volumeContext,
	}, false, nil
}

// DeleteNFSVolume deletes nfs volume which represents nfs pvc, deployment and service
func (flavor *Flavor) DeleteNFSVolume(pvName string) error {
	log.Tracef(">>>>> DeleteNFSVolume with %s", pvName)
	defer log.Tracef("<<<<< DeleteNFSVolume")

	// delete deployment deployment/hpe-nfs-<originalclaim-uid>
	resourceName := fmt.Sprintf("%s%s", nfsPrefix, strings.TrimPrefix(pvName, "pvc-"))
	err := flavor.DeleteNFSDeployment(resourceName)
	if err != nil {
		log.Errorf("unable to delete nfs deployment %s as part of cleanup, err %s", resourceName, err.Error())
	}

	// delete nfs pvc pvc/hpe-nfs-<originalclaim-uuid>
	// if deployment is still around, then pvc cannot be deleted due to protection, try to cleanup as best effort
	err = flavor.DeleteNFSPVC(resourceName)
	if err != nil {
		log.Errorf("unable to delete nfs pvc %s as part of cleanup, err %s", resourceName, err.Error())
	}

	// delete service service/hpe-nfs-<originalclaim-uid>
	err = flavor.DeleteNFSService(resourceName)
	if err != nil {
		log.Errorf("unable to delete nfs service %s as part of cleanup, err %s", resourceName, err.Error())
	}

	return err
}

func (flavor *Flavor) HandleNFSNodePublish(req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Tracef(">>>>> HandleNFSNodePublish with volume %s target path %s", req.VolumeId, req.TargetPath)
	defer log.Tracef("<<<<< HandleNFSNodePublish")

	// get nfs claim for corresponding nfs pv
	nfsResourceName := fmt.Sprintf("%s%s", nfsPrefix, req.VolumeId)

	// get service with matching volume-id(i.e original claim-id)
	service, err := flavor.GetNFSService(nfsResourceName)
	if err != nil {
		log.Errorf("unable to obtain service %s volume-id %s to publish volume", nfsResourceName, req.VolumeId)
		return nil, err
	}
	clusterIP := service.Spec.ClusterIP

	// TODO: add nfs mount support in chapi
	source := fmt.Sprintf("%s:%s", clusterIP, defaultExportPath)
	target := req.GetTargetPath()
	log.Debugf("mounting nfs volume %s to %s", source, target)
	opts := []string{
		"nolock",
		"intr",
		fmt.Sprintf("addr=%s", clusterIP),
	}
	if req.GetReadonly() {
		opts = append(opts, "ro")
	}
	options := strings.Join(opts, ",")

	log.Debugf("creating target path %s for nfs mount", target)
	if err := os.MkdirAll(target, 0750); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := syscall.Mount(source, target, "nfs4", 0, options); err != nil {
		if os.IsPermission(err) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if strings.Contains(err.Error(), "invalid argument") {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

// IsNFSVolume returns true if given volumeID belongs to nfs access volume
func (flavor *Flavor) IsNFSVolume(volumeID string) bool {
	// nfs pv, will have volume-id embedded in pv name
	_, err := flavor.getPvFromName(fmt.Sprintf("pvc-%s", volumeID))
	if err != nil {
		log.Tracef("unable to obtain pv based on volume-id %s", volumeID)
		return false
	}
	return true
}

func (flavor *Flavor) GetNFSSpec(scParams map[string]string) (*NFSSpec, error) {
	log.Tracef(">>>>> GetNFSSpec with %v", scParams)
	defer log.Tracef("<<<<< GetNFSSpec")

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
	nodes, err := flavor.GetNFSNodes()
	if err != nil {
		return nil, err
	}

	// use node-selector for deployment if we find nodes with hpe-nfs label
	if len(nodes) > 0 {
		nfsSpec.nodeSelector = map[string]string{nfsNodeSelectorKey: nfsNodeSelectorValue}
	}
	return &nfsSpec, nil
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

func (flavor *Flavor) cloneClaim(claim *core_v1.PersistentVolumeClaim) (*core_v1.PersistentVolumeClaim, error) {
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
	return claimClone, nil
}

// CreateNFSPVC creates Kubernetes Persistent Volume Claim
func (flavor *Flavor) CreateNFSPVC(claim *core_v1.PersistentVolumeClaim) (*core_v1.PersistentVolumeClaim, error) {
	log.Tracef(">>>>> CreateNFSPVC with claim %s", claim.ObjectMeta.Name)
	defer log.Tracef("<<<<< CreateNFSPVC")

	// check if nfs service already exists
	existingClaim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Get(claim.ObjectMeta.Name, meta_v1.GetOptions{})
	if err == nil && existingClaim != nil {
		log.Infof("nfs pvc %s exists in %s namespace", existingClaim.ObjectMeta.Name, nfsNamespace)
		return existingClaim, nil
	}

	// create new underlying nfs claim
	newClaim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Create(claim)
	if err != nil {
		return nil, err
	}

	// wait for pvc to be bound
	err = flavor.waitForPVCCreation(newClaim.ObjectMeta.Name)
	if err != nil {
		return nil, err
	}

	log.Infof("Completed creation of PVC %s", newClaim.ObjectMeta.Name)
	return newClaim, nil
}

// CreateNFSService creates a NFS service with given name
func (flavor *Flavor) CreateNFSService(svcName string) error {
	log.Tracef(">>>>> CreateNFSService with service name %s", svcName)
	defer log.Tracef("<<<<< CreateNFSService")

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
	service := flavor.makeNFSService(svcName)
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

func (flavor *Flavor) GetNFSNamespace(namespace string) (*core_v1.Namespace, error) {
	log.Tracef(">>>>> GetNFSNamespace with namespace name %s", namespace)
	defer log.Tracef("<<<<< GetNFSNamespace")

	ns, err := flavor.kubeClient.CoreV1().Namespaces().Get(namespace, meta_v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return ns, nil
}

func (flavor *Flavor) CreateNFSNamespace(namespace string) (*core_v1.Namespace, error) {
	log.Tracef(">>>>> CreateNFSNamespace with namespace name %s", namespace)
	defer log.Tracef("<<<<< CreateNFSNamespace")

	spec := &core_v1.Namespace{ObjectMeta: meta_v1.ObjectMeta{Name: namespace}}
	ns, err := flavor.kubeClient.CoreV1().Namespaces().Create(spec)
	if err != nil {
		return nil, err
	}
	return ns, nil
}

// GetNFSService :
func (flavor *Flavor) GetNFSService(svcName string) (*core_v1.Service, error) {
	log.Tracef(">>>>> GetNFSService with service name %s", svcName)
	defer log.Tracef("<<<<< GetNFSService")

	service, err := flavor.kubeClient.CoreV1().Services(nfsNamespace).Get(svcName, meta_v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

// GetNFSNodes returns nodes labeled to run HPE NFS Pods
func (flavor *Flavor) GetNFSNodes() ([]core_v1.Node, error) {
	log.Tracef(">>>>> GetNFSNodes")
	defer log.Tracef("<<<<< GetNFSNodes")

	// check if nfs service already exists
	nodeList, err := flavor.kubeClient.CoreV1().Nodes().List(meta_v1.ListOptions{LabelSelector: strings.Join([]string{nfsNodeSelectorKey, nfsNodeSelectorValue}, "=")})
	if err != nil {
		log.Errorf("unable to get list of nodes with hpe-nfs label, err %s", err.Error())
		return nil, err
	}
	return nodeList.Items, nil
}

// CreateNFSDeployment creates a nfs deployment with given name
func (flavor *Flavor) CreateNFSDeployment(deploymentName string, nfsSpec *NFSSpec) error {
	log.Tracef(">>>>> CreateNFSDeployment with name %s volume %s", deploymentName, nfsSpec.volumeClaim)
	defer log.Tracef("<<<<< CreateNFSDeployment")

	// validate nfs deployment settings
	if deploymentName == "" {
		return fmt.Errorf("empty name provided for creating NFS nfs deployment")
	}

	// check if nfs deployment already exists
	getAction := func() error {
		_, err := flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Get(deploymentName, meta_v1.GetOptions{})
		return err
	}
	exists, err := flavor.resourceExists(getAction, "deployment", deploymentName)
	if err == nil && exists {
		log.Infof("nfs deployment %s exists in %s namespace", deploymentName, nfsNamespace)
		return nil
	}

	// create a nfs deployment
	deployment := flavor.makeNFSDeployment(deploymentName, nfsSpec)
	if _, err := flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Create(deployment); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create nfs deployment %s, err %+v", deploymentName, err)
		}
		log.Infof("nfs deployment %s already exists", deploymentName)
		return nil
	}
	// make sure its available
	err = flavor.waitForDeployment(deploymentName)
	if err != nil {
		return err
	}
	// successfully created nfs deployment. make sure its running.
	log.Infof("nfs deployment %s started", deploymentName)

	return nil
}

//nolint
func (flavor *Flavor) makeNFSService(svcName string) *core_v1.Service {
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

func (flavor *Flavor) makeNFSDeployment(name string, nfsSpec *NFSSpec) *apps_v1.Deployment {
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

	podSpec := core_v1.PodTemplateSpec{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{"app": name},
			Annotations: map[string]string{"tags": name},
		},
		Spec: core_v1.PodSpec{
			Containers:    []core_v1.Container{flavor.makeContainer("hpe-nfs", nfsSpec)},
			RestartPolicy: core_v1.RestartPolicyAlways,
			Volumes:       volumes,
			HostIPC:       false,
			HostNetwork:   false,
		},
	}

	// apply if any node selector is specified by user
	if len(nfsSpec.nodeSelector) != 0 {
		podSpec.Spec.NodeSelector = nfsSpec.nodeSelector
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
		},
	}
	return d
}

// nolint
func (flavor *Flavor) makeContainer(name string, nfsSpec *NFSSpec) core_v1.Container {
	log.Tracef(">>>>> makeContainer with name %s, volume %s", name, nfsSpec.volumeClaim)
	defer log.Tracef("<<<<< makeContainer")

	securityContext := &core_v1.SecurityContext{
		Privileged: boolToPtr(true),
		Capabilities: &core_v1.Capabilities{
			Add: []core_v1.Capability{"SYS_ADMIN", "DAC_READ_SEARCH"},
		},
	}

	cont := core_v1.Container{
		Name:            name,
		Image:           nfsImage,
		ImagePullPolicy: core_v1.PullAlways,
		SecurityContext: securityContext,
		Env: []core_v1.EnvVar{
			{
				Name:  "GANESHA_OPTIONS",
				Value: "-N NIV_DEBUG",
			},
			{
				Name:  "GANESHA_CONFIGFILE",
				Value: "/etc/ganesha.conf",
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
		},
	}

	// apply if any resource requirements are specified by user
	if nfsSpec.resourceRequirements != nil {
		cont.Resources = *nfsSpec.resourceRequirements
	}

	return cont
}

// DeleteNFSService deletes NFS service and its depending artifacts
func (flavor *Flavor) DeleteNFSService(svcName string) error {
	log.Tracef(">>>>> DeleteNFSService with service %s", svcName)
	defer log.Tracef("<<<<< DeleteNFSService")

	// check if service  exists
	getAction := func() error {
		_, err := flavor.kubeClient.CoreV1().Services(nfsNamespace).Get(svcName, meta_v1.GetOptions{})
		return err
	}
	exists, err := flavor.resourceExists(getAction, "service", svcName)
	if err != nil {
		return fmt.Errorf("failed to detect if there is a NFS service to delete. %+v", err)
	}
	if !exists {
		log.Infof("nfs service %s does not exist in %s namespace", svcName, nfsNamespace)
		return nil
	}

	log.Infof("Deleting nfs service %s from %s namespace", svcName, nfsNamespace)

	var gracePeriod int64
	propagation := meta_v1.DeletePropagationForeground
	options := &meta_v1.DeleteOptions{GracePeriodSeconds: &gracePeriod, PropagationPolicy: &propagation}

	// Delete the nfs service
	err = flavor.kubeClient.CoreV1().Services(nfsNamespace).Delete(svcName, options)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("failed to delete nfs service %s, err %+v", svcName, err)
	}

	log.Infof("Completed deletion of nfs service %s", svcName)
	return nil
}

// DeleteNFSDeployment deletes NFS service and its depending artifacts
func (flavor *Flavor) DeleteNFSDeployment(name string) error {
	log.Tracef(">>>>> DeleteDeployment with %s", name)
	defer log.Tracef("<<<<< DeleteDeployment")

	deleteAction := func(options *meta_v1.DeleteOptions) error {
		return flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Delete(name, options)
	}
	getAction := func() error {
		_, err := flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Get(name, meta_v1.GetOptions{})
		return err
	}
	err := flavor.deleteResourceAndWait(nfsNamespace, name, "deployment", deleteAction, getAction)
	if err != nil {
		return err
	}
	log.Infof("Completed deletion of nfs deployment %s", name)
	return nil
}

// DeleteNFSPVC deletes NFS service and its depending artifacts
func (flavor *Flavor) DeleteNFSPVC(claimName string) error {
	log.Tracef(">>>>> DeletePVC with %s", claimName)
	defer log.Tracef("<<<<< DeletePVC")

	// Delete the pvc
	deleteAction := func(options *meta_v1.DeleteOptions) error {
		return flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Delete(claimName, options)
	}
	getAction := func() error {
		_, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(nfsNamespace).Get(claimName, meta_v1.GetOptions{})
		return err
	}
	err := flavor.deleteResourceAndWait(nfsNamespace, claimName, "persistentvolumeclaim", deleteAction, getAction)
	if err != nil {
		return err
	}
	log.Infof("Completed deletion of PVC %s", claimName)
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

func (flavor *Flavor) deploymentExists(deploymentName string) (bool, error) {
	log.Tracef(">>>>> deploymentExists with %s", deploymentName)
	defer log.Tracef("<<<<< deploymentExists")

	_, err := flavor.kubeClient.AppsV1().Deployments(nfsNamespace).Get(deploymentName, meta_v1.GetOptions{})
	if err == nil {
		// the deployment was found
		return true, nil
	}
	if !errors.IsNotFound(err) {
		return false, err
	}

	// deployment not found
	return false, nil
}

// Check if the NFS service exists
func (flavor *Flavor) serviceExists(svcName string) (bool, error) {
	log.Tracef(">>>>> serviceExists with %s", svcName)
	defer log.Tracef("<<<<< serviceExists")

	_, err := flavor.kubeClient.CoreV1().Services(nfsNamespace).Get(svcName, meta_v1.GetOptions{})
	if err == nil {
		// the deployment was found
		return true, nil
	}
	if !errors.IsNotFound(err) {
		return false, err
	}

	// service not found
	return false, err
}

// deleteResourceAndWait will delete a resource, then wait for it to be purged from the system
func (flavor *Flavor) deleteResourceAndWait(namespace, name, resourceType string,
	deleteAction func(*meta_v1.DeleteOptions) error,
	getAction func() error,
) error {
	log.Tracef(">>>>> deleteResourceAndWait with %s %s", resourceType, name)
	defer log.Tracef("<<<<< deleteResourceAndWait")

	var gracePeriod int64
	propagation := meta_v1.DeletePropagationForeground
	options := &meta_v1.DeleteOptions{GracePeriodSeconds: &gracePeriod, PropagationPolicy: &propagation}

	// Delete the resource if it exists
	log.Infof("removing %s %s if it exists", resourceType, name)
	err := deleteAction(options)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %s %s, err %+v", resourceType, name, err)
		}
		return nil
	}
	log.Infof("Removed %s %s", resourceType, name)

	// wait for the resource to be deleted
	sleepTime := deletionDelay
	for i := 0; i < deletionInterval; i++ {
		// check for the existence of the resource
		err = getAction()
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof("confirmed %s %s does not exist", resourceType, name)
				return nil
			}
			return fmt.Errorf("failed to get %s %s. %+v", resourceType, name, err)
		}

		log.Infof("%s %s still found. waiting(try %d)...", resourceType, name, i+1)
		time.Sleep(sleepTime)
	}
	return fmt.Errorf("gave up waiting for %s %s to be terminated", resourceType, name)
}

func (flavor *Flavor) waitForPVCCreation(claimName string) error {
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
		}
		// successfully bound
		return nil
	}
	return fmt.Errorf("gave up waiting for pvc %s to be bound", claimName)
}

func (flavor *Flavor) waitForDeployment(deploymentName string) error {
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
			log.Infof("pvc %s still not bound to pv. waiting(try %d)...", deploymentName, i+1)
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

func boolToPtr(b bool) *bool {
	return &b
}
