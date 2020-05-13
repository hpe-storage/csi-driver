// Copyright 2020 Hewlett Packard Enterprise Development LP

package kubernetes

import (
	"fmt"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"k8s.io/api/core/v1"
	storage_v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	defaultIntervalSec   = 30
	minimumIntervalSec   = 15
	nodeUnreachableTaint = "node.kubernetes.io/unreachable"
	nodeLostCondition    = "NodeLost"
)

// Monitor Pods running on un-reachable nodes
type Monitor struct {
	IntervalSec int64
	lock        sync.Mutex
	started     bool
	stopChannel chan int
	done        chan int
}

// StartNFSMonitor Starts the monitor
func (flavor *Flavor) StartNFSMonitor() error {
	log.Trace(">>>>> StartNFSMonitor")
	defer log.Trace("<<<<< StartNFSMonitor")

	flavor.nfsMonitor.lock.Lock()
	defer flavor.nfsMonitor.lock.Unlock()

	if flavor.nfsMonitor.started {
		return fmt.Errorf("Monitor has already been started")
	}

	if flavor.nfsMonitor.IntervalSec == 0 {
		flavor.nfsMonitor.IntervalSec = defaultIntervalSec
	} else if flavor.nfsMonitor.IntervalSec < minimumIntervalSec {
		log.Warnf("minimum interval for health monitor is %v seconds", minimumIntervalSec)
		flavor.nfsMonitor.IntervalSec = minimumIntervalSec
	}

	flavor.nfsMonitor.stopChannel = make(chan int)
	flavor.nfsMonitor.done = make(chan int)

	if err := flavor.nfsPodMonitor(); err != nil {
		return err
	}

	flavor.nfsMonitor.started = true
	return nil
}

// StopNFSMonitor Stops the monitor
func (flavor *Flavor) StopNFSMonitor() error {
	log.Trace(">>>>> StopNFSMonitor")
	defer log.Trace("<<<<< StopNFSMonitor")

	flavor.nfsMonitor.lock.Lock()
	defer flavor.nfsMonitor.lock.Unlock()

	if !flavor.nfsMonitor.started {
		return fmt.Errorf("Monitor has not been started")
	}

	close(flavor.nfsMonitor.stopChannel)
	<-flavor.nfsMonitor.done

	flavor.nfsMonitor.started = false
	return nil
}

func (flavor *Flavor) nfsPodMonitor() error {
	log.Trace(">>>>> nfsPodMonitor")
	defer log.Trace("<<<<< nfsPodMonitor")
	defer close(flavor.nfsMonitor.done)

	tick := time.NewTicker(time.Duration(flavor.nfsMonitor.IntervalSec) * time.Second)

	go func() {
		for {
			select {
			case <-tick.C:
				err := flavor.monitor()
				if err != nil {
					log.Errorf("nfs pod monitoring failed with error %s", err.Error())
				}
			case <-flavor.nfsMonitor.stopChannel:
				return
			}
		}
	}()
	return nil
}

// monitors nfs pods for being in unknown state(node unreachable/lost) and assist them to move to different node
func (flavor *Flavor) monitor() error {
	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{nfsPodLabelKey: nfsPodLabelValue}}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}

	podList, err := flavor.kubeClient.CoreV1().Pods(metav1.NamespaceAll).List(listOptions)
	if err != nil {
		log.Errorf("unable to list nfs pods for monitoring: %s", err.Error())
		return err
	}
	if podList == nil || len(podList.Items) == 0 {
		log.Errorf("cannot find any nfs pod with label %s=%s", nfsPodLabelKey, nfsPodLabelValue)
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
