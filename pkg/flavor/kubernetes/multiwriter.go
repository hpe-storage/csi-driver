// Copyright 2019 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/hpe-storage/common-host-libs/logger"
	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/util/mount"
	"os"
	"strings"
	"time"
)

const (
	serviceNamePrefix    = "hpe-multiwriter-svc-"
	deploymentNamePrefix = "hpe-multiwriter-"
	pvcNamePrefix        = "hpe-multiwriter-pvc"
	multiWriterNamespace = "kube-system"
	nfsImage             = "quay.io/luis_pabon0/ganesha" // TODO evaluate latest nfs-ganesha image
	deletionInterval     = 30                            // 60s with sleep interval of 2s
	deletionDelay        = 2 * time.Second
	creationInterval     = 60 // 120s with sleep interval of 2s
	creationDelay        = 2 * time.Second
	// defaultVolumeSize is the implementation-specific default value in bytes
	defaultVolumeSize = 10 * 1024 * 1024 * 1024 // 10 GiB
	//defaultExportPath = "/exports"
)

// CreateMultiWriterVolume creates multi-writer volume abstracting underlying nfs pvc, deployment and service
func (flavor *Flavor) CreateMultiWriterVolume(request *csi.CreateVolumeRequest) (multiWriterVolume *csi.Volume, rollback bool, err error) {
	log.Tracef(">>>>> CreateMultiWriterVolume with %s", request.Name)
	defer log.Tracef("<<<<< CreateMultiWriterVolume")

	// get pvc from request
	claim, err := flavor.getClaimFromClaimName(request.Name)
	if err != nil {
		return nil, false, err
	}

	// clone pvc and modify to RWO mode and destroyOnDelete
	claimClone, err := flavor.makeNFSPvc(claim)
	if err != nil {
		return nil, false, err
	}

	// create pvc
	newClaim, err := flavor.CreateNFSPVC(claimClone)
	if err != nil {
		return nil, true, err
	}

	// create deployment with name hpe-multiwriter-<originalclaim-uid>
	deploymentName := fmt.Sprintf("%s%s", deploymentNamePrefix, claim.ObjectMeta.UID)
	err = flavor.CreateNFSDeployment(deploymentName, newClaim.ObjectMeta.Name)
	if err != nil {
		return nil, true, err
	}

	// create service with name hpe-multiwriter-svc-<originalclaim-uid>
	serviceName := fmt.Sprintf("%s%s", serviceNamePrefix, claim.ObjectMeta.UID)
	err = flavor.CreateNFSService(serviceName)
	if err != nil {
		return nil, true, err
	}

	// Get capacity requested
	reqVolumeSize := int64(defaultVolumeSize) // Default value
	if request.GetCapacityRange() != nil {
		reqVolumeSize = request.GetCapacityRange().GetRequiredBytes()
	}

	// Return newly created volume
	return &csi.Volume{
		VolumeId:      fmt.Sprintf("%s", claim.ObjectMeta.UID),
		CapacityBytes: reqVolumeSize,
		VolumeContext: make(map[string]string),
	}, false, nil
}

// DeleteMultiWriterVolume deletes multi-writer volume which represents nfs pvc, deployment and service
func (flavor *Flavor) DeleteMultiWriterVolume(claimName string) error {
	log.Tracef(">>>>> DeleteMultiWriterVolume with %s", claimName)
	defer log.Tracef("<<<<< DeleteMultiWriterVolume")

	// get pvc from request
	claim, err := flavor.getClaimFromClaimName(claimName)
	if err != nil {
		return err
	}
	// delete service with name hpe-multiwriter-svc-<originalclaim-uid>
	serviceName := fmt.Sprintf("%s%s", serviceNamePrefix, claim.ObjectMeta.UID)
	err = flavor.DeleteNFSService(serviceName)
	if err != nil {
		log.Errorf("unable to delete nfs service %s as part of cleanup, err %s", serviceName, err.Error())
	}

	// delete deployment with name hpe-multiwriter-<originalclaim-uid>
	deploymentName := fmt.Sprintf("%s%s", deploymentNamePrefix, claim.ObjectMeta.UID)
	err = flavor.DeleteNFSDeployment(deploymentName)
	if err != nil {
		log.Errorf("unable to delete nfs deployment %s as part of cleanup, err %s", deploymentName, err.Error())
	}
	// delete pvc with name hpe-multiwriter-pvc-<originalclaim-uid>
	// if deployment is still around, then pvc cannot be deleted due to protection, try to cleanup as best effort
	pvcName := fmt.Sprintf("%s-%s", pvcNamePrefix, claim.ObjectMeta.UID)
	err = flavor.DeleteNFSPVC(pvcName)
	if err != nil {
		log.Errorf("unable to delete nfs pvc %s as part of cleanup, err %s", pvcName, err.Error())
	}
	return err
}

func (flavor *Flavor) HandleMultiWriterNodePublish(req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Tracef(">>>>> HandleMultiWriterNodePublish with volume %s target path %s", req.VolumeId, req.TargetPath)
	defer log.Tracef("<<<<< HandleMultiWriterNodePublish")

	// get service with matching volume-id(i.e original claim-id)
	serviceName := fmt.Sprintf("%s%s", serviceNamePrefix, req.VolumeId)
	service, err := flavor.GetNFSService(serviceName)
	if err != nil {
		log.Errorf("unable to obtain service %s volume-id %s to publish volume", serviceName, req.VolumeId)
		return err
	}
	clusterIP := service.Spec.ClusterIP

	// TODO: add nfs mount support in chapi
	targetPath := req.GetTargetPath()
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(targetPath, 0750); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	if !notMnt {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	mo := req.GetVolumeCapability().GetMount().GetMountFlags()
	if req.GetReadonly() {
		mo = append(mo, "ro")
	}

	source := fmt.Sprintf("%s:%s", clusterIP, defaultExportPath)
	mounter := mount.New("")
	err = mounter.Mount(source, targetPath, "nfs", mo)
	if err != nil {
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

func (flavor *Flavor) IsMultiWriterVolume(volumeID string) bool {
	serviceName := fmt.Sprintf("%s%s", serviceNamePrefix, volumeID)
	_, err := flavor.GetNFSService(serviceName)
	if err != nil {
		log.Debugf("unable to obtain service %s volume-id %s", serviceName, volumeID)
		return false
	}
	return true
}

func (flavor *Flavor) GetMultiWriterClaimFromClaimUID(uid string) (*core_v1.PersistentVolumeClaim, error) {
	log.Tracef(">>>>> GetMultiWriterClaimFromClaimUid with claim %s", uid)
	defer log.Tracef("<<<<< GetMultiWriterClaimFromClaimUid")

	claims, err := flavor.claimIndexer.ByIndex("uid", uid)
	if err != nil {
		return nil, fmt.Errorf("Failed to retrieve claim by uid %s", uid)
	} else if len(claims) == 0 {
		return nil, fmt.Errorf("Requested pvc was not found with uid %s", uid)
	}

	log.Infof("Found the following claims: %v", claims)
	return claims[0].(*core_v1.PersistentVolumeClaim), nil
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

func (flavor *Flavor) makeNFSPvc(claim *core_v1.PersistentVolumeClaim) (*core_v1.PersistentVolumeClaim, error) {
	log.Tracef(">>>>> makeNFSPvc with claim %s", claim.ObjectMeta.Name)
	defer log.Tracef("<<<<< makeNFSPvc")

	// get copy of original pvc
	claimClone := claim.DeepCopy()
	claimClone.ObjectMeta.Namespace = multiWriterNamespace
	claimClone.ObjectMeta.ResourceVersion = ""
	// modify name with hpe-multiwriter-pvc-<originalclaim-uid>
	claimClone.ObjectMeta.Name = fmt.Sprintf("%s-%s", pvcNamePrefix, claim.ObjectMeta.UID)
	// change access-mode to RWO for underlying nfs pvc
	claimClone.Spec.AccessModes = []core_v1.PersistentVolumeAccessMode{core_v1.ReadWriteOnce}
	return claimClone, nil
}

// Create Kubernetes Persistent Volume Claim
func (flavor *Flavor) CreateNFSPVC(claim *core_v1.PersistentVolumeClaim) (*core_v1.PersistentVolumeClaim, error) {
	log.Tracef(">>>>> CreateNFSPVC with claim %s", claim.ObjectMeta.Name)
	defer log.Tracef("<<<<< CreateNFSPVC")

	newClaim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(multiWriterNamespace).Create(claim)
	if err != nil {
		return nil, err
	}

	// wait for pvc to be bound
	err = flavor.waitForPvcCreation(newClaim.ObjectMeta.Name)
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
		return fmt.Errorf("empty service name provided for creating nfs multiwriter service")
	}

	// check if nfs service already exists
	getAction := func() error {
		_, err := flavor.kubeClient.CoreV1().Services(multiWriterNamespace).Get(svcName, meta_v1.GetOptions{})
		return err
	}
	exists, err := flavor.resourceExists(getAction, "service", svcName)
	if err == nil && exists {
		log.Infof("nfs service %s exists in %s namespace", svcName, multiWriterNamespace)
		return nil
	}

	// create the nfs service
	service := flavor.makeNFSService(svcName)
	if _, err := flavor.kubeClient.CoreV1().Services(multiWriterNamespace).Create(service); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create nfs service %s, err %+v", svcName, err)
		}
		log.Infof("nfs service %s already exists", svcName)
	} else {
		log.Infof("nfs service %s started", svcName)
	}
	return nil
}

func (flavor *Flavor) GetNFSService(svcName string) (*core_v1.Service, error) {
	log.Tracef(">>>>> GetNFSService with service name %s", svcName)
	defer log.Tracef("<<<<< GetNFSService")

	service, err := flavor.kubeClient.CoreV1().Services(multiWriterNamespace).Get(svcName, meta_v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

// CreateNFSDeployment creates a nfs deployment with given name
func (flavor *Flavor) CreateNFSDeployment(deploymentName, pvcName string) error {
	log.Tracef(">>>>> CreateNFSDeployment with name %s", deploymentName)
	defer log.Tracef("<<<<< CreateNFSDeployment")

	// validate nfs deployment settings
	if deploymentName == "" {
		return fmt.Errorf("empty name provided for creating NFS multiwriter deployment")
	}

	// check if nfs deployment already exists
	getAction := func() error {
		_, err := flavor.kubeClient.AppsV1().Deployments(multiWriterNamespace).Get(deploymentName, meta_v1.GetOptions{})
		return err
	}
	exists, err := flavor.resourceExists(getAction, "deployment", deploymentName)
	if err == nil && exists {
		log.Infof("nfs deployment %s exists in %s namespace", deploymentName, multiWriterNamespace)
		return nil
	}

	// create a nfs deployment
	deployment := flavor.makeNFSDeployment(deploymentName, pvcName)
	if _, err := flavor.kubeClient.AppsV1().Deployments(multiWriterNamespace).Create(deployment); err != nil {
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

	labels := map[string]string{"app": getDeploymentFromServiceName(svcName)}
	svc := &core_v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      svcName,
			Namespace: multiWriterNamespace,
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

func (flavor *Flavor) makeNFSDeployment(name, volumeClaim string) *apps_v1.Deployment {
	log.Tracef(">>>>> makeNFSDeployment with name %s, pvc %s", name, volumeClaim)
	defer log.Tracef("<<<<< makeNFSDeployment")

	volumes := []core_v1.Volume{}
	// add PVC
	volumes = append(volumes, core_v1.Volume{
		Name: volumeClaim,
		VolumeSource: core_v1.VolumeSource{
			PersistentVolumeClaim: &core_v1.PersistentVolumeClaimVolumeSource{
				ClaimName: volumeClaim,
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
			Containers:    []core_v1.Container{flavor.makeContainer("hpe-multiwriter", volumeClaim)},
			RestartPolicy: core_v1.RestartPolicyAlways,
			Volumes:       volumes,
			HostIPC:       true,
			HostNetwork:   true,
		},
	}
	podSpec.Spec.DNSPolicy = core_v1.DNSClusterFirstWithHostNet

	d := &apps_v1.Deployment{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name,
			Namespace: multiWriterNamespace,
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
func (flavor *Flavor) makeContainer(name, volumeName string) core_v1.Container {
	log.Tracef(">>>>> makeContainer with name %s, volume %s", name, volumeName)
	defer log.Tracef("<<<<< makeContainer")

	securityContext := &core_v1.SecurityContext{
		Privileged: boolToPtr(true),
	}

	cont := core_v1.Container{
		Name:            name,
		Image:           nfsImage,
		ImagePullPolicy: core_v1.PullAlways,
		SecurityContext: securityContext,
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
				Name:      volumeName,
				MountPath: "/exports",
			},
		},
	}
	return cont
}

// Delete NFS service and its depending artifacts
func (flavor *Flavor) DeleteNFSService(svcName string) error {
	log.Tracef(">>>>> DeleteNFSService with service %s", svcName)
	defer log.Tracef("<<<<< DeleteNFSService")

	// check if service  exists
	getAction := func() error {
		_, err := flavor.kubeClient.CoreV1().Services(multiWriterNamespace).Get(svcName, meta_v1.GetOptions{})
		return err
	}
	exists, err := flavor.resourceExists(getAction, "service", svcName)
	if err != nil {
		return fmt.Errorf("failed to detect if there is a NFS service to delete. %+v", err)
	}
	if !exists {
		log.Infof("nfs service %s does not exist in kube-system namespace", svcName)
		return nil
	}

	log.Infof("Deleting nfs service %s from kube-system namespace", svcName)

	var gracePeriod int64
	propagation := meta_v1.DeletePropagationForeground
	options := &meta_v1.DeleteOptions{GracePeriodSeconds: &gracePeriod, PropagationPolicy: &propagation}

	// Delete the nfs service
	err = flavor.kubeClient.CoreV1().Services(multiWriterNamespace).Delete(svcName, options)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("failed to delete nfs service %s, err %+v", svcName, err)
	}

	log.Infof("Completed deletion of nfs service %s", svcName)
	return nil
}

// Delete NFS service and its depending artifacts
func (flavor *Flavor) DeleteNFSDeployment(name string) error {
	log.Tracef(">>>>> DeleteDeployment with %s", name)
	defer log.Tracef("<<<<< DeleteDeployment")

	deleteAction := func(options *meta_v1.DeleteOptions) error {
		return flavor.kubeClient.AppsV1().Deployments(multiWriterNamespace).Delete(name, options)
	}
	getAction := func() error {
		_, err := flavor.kubeClient.AppsV1().Deployments(multiWriterNamespace).Get(name, meta_v1.GetOptions{})
		return err
	}
	err := flavor.deleteResourceAndWait(multiWriterNamespace, name, "deployment", deleteAction, getAction)
	if err != nil {
		return err
	}
	log.Infof("Completed deletion of nfs deployment %s", name)
	return nil
}

// Delete NFS service and its depending artifacts
func (flavor *Flavor) DeleteNFSPVC(claimName string) error {
	log.Tracef(">>>>> DeletePVC with %s", claimName)
	defer log.Tracef("<<<<< DeletePVC")

	// Delete the pvc
	deleteAction := func(options *meta_v1.DeleteOptions) error {
		return flavor.kubeClient.CoreV1().PersistentVolumeClaims(multiWriterNamespace).Delete(claimName, options)
	}
	getAction := func() error {
		_, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(multiWriterNamespace).Get(claimName, meta_v1.GetOptions{})
		return err
	}
	err := flavor.deleteResourceAndWait(multiWriterNamespace, claimName, "persistentvolumeclaim", deleteAction, getAction)
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

	_, err := flavor.kubeClient.AppsV1().Deployments(multiWriterNamespace).Get(deploymentName, meta_v1.GetOptions{})
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

	_, err := flavor.kubeClient.CoreV1().Services(multiWriterNamespace).Get(svcName, meta_v1.GetOptions{})
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

func (flavor *Flavor) waitForPvcCreation(claimName string) error {
	log.Tracef(">>>>> waitForPvcCreation with %s", claimName)
	defer log.Tracef("<<<<< waitForPvcCreation")

	// wait for the resource to be deleted
	sleepTime := creationDelay
	for i := 0; i < creationInterval; i++ {
		// check for the existence of the resource
		claim, err := flavor.kubeClient.CoreV1().PersistentVolumeClaims(multiWriterNamespace).Get(claimName, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pvc %s, err %+v", claimName, err)
		}
		if claim.Status.Phase != core_v1.ClaimBound {
			log.Infof("pvc %s still not bound to pv. waiting(try %d)...", claimName, i+1)
			time.Sleep(sleepTime)
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
		deployment, err := flavor.kubeClient.AppsV1().Deployments(multiWriterNamespace).Get(deploymentName, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment %s, err %+v", deploymentName, err)
		}
		if deployment.Status.AvailableReplicas != 1 {
			log.Infof("pvc %s still not bound to pv. waiting(try %d)...", deploymentName, i+1)
			time.Sleep(sleepTime)
		}
		// successfully running
		return nil
	}
	return fmt.Errorf("gave up waiting for deployment %s to be available", deploymentName)
}

func getDeploymentFromServiceName(svcName string) string {
	log.Tracef(">>>>> getDeploymentFromServiceName with %s", svcName)
	defer log.Tracef("<<<<< getDeploymentFromServiceName")
	// remove service name prefix and add deployment prefix
	return deploymentNamePrefix + strings.TrimPrefix(svcName, serviceNamePrefix)
}

func int32toPtr(i int32) *int32 {
	return &i
}

func boolToPtr(b bool) *bool {
	return &b
}
