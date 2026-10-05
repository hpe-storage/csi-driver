// Copyright 2026 Hewlett Packard Enterprise Development LP

package driver

import (
	"context"
	"os"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/csi-driver/pkg/monitor"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// podMonitorLeaseName is the Lease that gates the (otherwise unconditionally run) podMonitor loop
// to a single controller replica, per CON-4960-25.
const podMonitorLeaseName = "hpe-csi-driver-podmonitor-leader"

// Default Lease timings, overridable via PODMONITOR_LEADER_ELECTION_* env vars (see getLeaderElectionDuration).
const (
	defaultPodMonitorLeaseDuration = 15 * time.Second
	defaultPodMonitorRenewDeadline = 10 * time.Second
	defaultPodMonitorRetryPeriod   = 2 * time.Second
)

// getLeaderElectionDuration reads a Lease timing from envVarName (e.g. "15s"), falling back to
// defaultVal if unset or invalid.
func getLeaderElectionDuration(envVarName string, defaultVal time.Duration) time.Duration {
	if envVal := os.Getenv(envVarName); envVal != "" {
		if parsed, err := time.ParseDuration(envVal); err == nil && parsed > 0 {
			return parsed
		}
		log.Warnf("Invalid %s=%q, using default %s", envVarName, envVal, defaultVal)
	}
	return defaultVal
}

// podMonitorCallbacks builds the leader-election lifecycle callbacks for the podMonitor Lease.
// OnStoppedLeading must never exit the process: hpe-csi-driver also serves the local CSI gRPC
// socket for whichever co-located sidecar is leading, so losing this Lease must only stop the
// monitor loop (R-02).
func podMonitorCallbacks(podMonitor *monitor.Monitor) leaderelection.LeaderCallbacks {
	return leaderelection.LeaderCallbacks{
		OnStartedLeading: func(_ context.Context) {
			log.Info("Acquired hpe-csi-driver-podmonitor-leader lease; starting pod monitor")
			if err := podMonitor.StartMonitor(); err != nil {
				log.Errorf("Failed to start pod monitor: %s", err.Error())
			}
		},
		OnStoppedLeading: func() {
			log.Info("Lost/released hpe-csi-driver-podmonitor-leader lease; stopping pod monitor")
			if err := podMonitor.StopMonitor(); err != nil {
				log.Errorf("Failed to stop pod monitor: %s", err.Error())
			}
		},
	}
}

// newPodMonitorLeaderElector builds the Lease-based elector for the podMonitor loop. clientset is
// injected so tests can pass a fake, without needing a real cluster.
func newPodMonitorLeaderElector(namespace, identity string, clientset k8sclient.Interface, podMonitor *monitor.Monitor) (*leaderelection.LeaderElector, error) {
	return leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      podMonitorLeaseName,
				Namespace: namespace,
			},
			Client: clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: identity,
			},
		},
		LeaseDuration: getLeaderElectionDuration("PODMONITOR_LEADER_ELECTION_LEASE_DURATION", defaultPodMonitorLeaseDuration),
		RenewDeadline: getLeaderElectionDuration("PODMONITOR_LEADER_ELECTION_RENEW_DEADLINE", defaultPodMonitorRenewDeadline),
		RetryPeriod:   getLeaderElectionDuration("PODMONITOR_LEADER_ELECTION_RETRY_PERIOD", defaultPodMonitorRetryPeriod),
		Callbacks:     podMonitorCallbacks(podMonitor),
	})
}

// newInClusterPodMonitorLeaderElector builds the podMonitor elector using in-cluster config and
// the pod's hostname as holder identity. Returns an error if any precondition (API access,
// hostname) can't be satisfied; the caller must treat this as "disable podMonitor entirely".
func newInClusterPodMonitorLeaderElector(namespace string, podMonitor *monitor.Monitor) (*leaderelection.LeaderElector, error) {
	clientset, identity, err := newInClusterKubeClientAndIdentity()
	if err != nil {
		return nil, err
	}
	return newPodMonitorLeaderElector(namespace, identity, clientset, podMonitor)
}

// newInClusterKubeClientAndIdentity builds an in-cluster Kubernetes clientset plus a holder
// identity (the pod's hostname), shared by the podMonitor Lease and the distributed dedup Lease
// (CON-4960-25/-26) so the process only ever builds one in-cluster clientset.
func newInClusterKubeClientAndIdentity() (k8sclient.Interface, string, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, "", err
	}
	clientset, err := k8sclient.NewForConfig(cfg)
	if err != nil {
		return nil, "", err
	}
	identity, err := os.Hostname()
	if err != nil {
		return nil, "", err
	}
	return clientset, identity, nil
}
