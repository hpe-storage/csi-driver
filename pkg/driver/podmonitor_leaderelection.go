// Copyright 2026 Hewlett Packard Enterprise Development LP

package driver

import (
	"context"
	"time"

	"github.com/hpe-storage/common-host-libs/distributedlock"
	"github.com/hpe-storage/common-host-libs/leaderlease"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/csi-driver/pkg/monitor"
	k8sclient "k8s.io/client-go/kubernetes"
)

// podMonitorLeaseName is the Lease that gates the (otherwise unconditionally run) podMonitor loop
// to a single controller replica, per CON-4982.
const podMonitorLeaseName = "hpe-csi-driver-podmonitor-leader"

// Default Lease timings, overridable via PODMONITOR_LEADER_ELECTION_* env vars (see
// distributedlock.GetEnvDuration).
const (
	defaultPodMonitorLeaseDuration = 15 * time.Second
	defaultPodMonitorRenewDeadline = 10 * time.Second
	defaultPodMonitorRetryPeriod   = 2 * time.Second
)

// newPodMonitorLeaderElector builds the Lease-based elector for the podMonitor loop. clientset is
// injected so tests can pass a fake, without needing a real cluster.
//
// ExitOnUnexpectedLoss is false: hpe-csi-driver also serves the local CSI gRPC socket for
// whichever co-located sidecar is leading, so losing this Lease must only stop the monitor loop
// and re-contest in-process (CON-4982 H1), never exit the process.
func newPodMonitorLeaderElector(namespace, identity string, clientset k8sclient.Interface, podMonitor *monitor.Monitor) (*leaderlease.Elector, error) {
	return leaderlease.New(clientset, identity, leaderlease.Config{
		LeaseName:     podMonitorLeaseName,
		Namespace:     namespace,
		LeaseDuration: distributedlock.GetEnvDuration("PODMONITOR_LEADER_ELECTION_LEASE_DURATION", defaultPodMonitorLeaseDuration),
		RenewDeadline: distributedlock.GetEnvDuration("PODMONITOR_LEADER_ELECTION_RENEW_DEADLINE", defaultPodMonitorRenewDeadline),
		RetryPeriod:   distributedlock.GetEnvDuration("PODMONITOR_LEADER_ELECTION_RETRY_PERIOD", defaultPodMonitorRetryPeriod),
		// leaderlease already logs the acquire/lose transition itself; these callbacks only need
		// to log their own StartMonitor/StopMonitor failures.
		OnStartedLeading: func(_ context.Context) {
			if err := podMonitor.StartMonitor(); err != nil {
				log.Errorf("Failed to start pod monitor: %s", err.Error())
			}
		},
		OnStoppedLeading: func(_ bool) {
			if err := podMonitor.StopMonitor(); err != nil {
				log.Errorf("Failed to stop pod monitor: %s", err.Error())
			}
		},
		ExitOnUnexpectedLoss: false,
	})
}

// newInClusterPodMonitorLeaderElector builds the podMonitor elector using in-cluster config and
// the pod's hostname as holder identity. Returns an error if any precondition (API access,
// hostname) can't be satisfied; the caller must treat this as "disable podMonitor entirely".
func newInClusterPodMonitorLeaderElector(namespace string, podMonitor *monitor.Monitor) (*leaderlease.Elector, error) {
	clientset, identity, err := distributedlock.NewInClusterKubeClientAndIdentity()
	if err != nil {
		return nil, err
	}
	return newPodMonitorLeaderElector(namespace, identity, clientset, podMonitor)
}
