// Copyright 2026 Hewlett Packard Enterprise Development LP

package driver

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
)

// Cross-pod duplicate-request dedup via one Kubernetes Lease per request key (CON-4982),
// used when no dbservice.DBService is configured (the common case). Falls back further to the
// in-process sync.Map when preconditions aren't met — duplicate-request detection is never
// disabled outright, unlike podMonitor.
const (
	dedupLeaseNamePrefix = "hpe-csi-dedup-"
	dedupLeaseLabelKey   = "storage.hpe.com/dedup-lock"
	dedupLeaseLabelValue = "true"
	dedupLeaseNameMaxLen = 253
)

// Default timings, overridable via DEDUP_LOCK_TTL/DEDUP_REAPER_INTERVAL env vars (see
// getLeaderElectionDuration in podmonitor_leaderelection.go). Reaper interval defaults much
// longer than the TTL — it's pure housekeeping for rare, never-retried orphans (see
// TryAcquire's steal-on-acquire, which alone guarantees correctness), so it runs infrequently to
// minimize List calls (every controller replica runs its own reaper, unlike the leader-elected
// podMonitor).
const (
	defaultDedupLockTTL        = 60 * time.Second
	defaultDedupReaperInterval = 15 * time.Minute
)

// errDuplicateInFlight is returned by TryAcquire when a non-stale Lease for the key already exists.
var errDuplicateInFlight = errors.New("duplicate request already in flight")

// distributedDedup gates HandleDuplicateRequest/ClearRequest across controller replicas using a
// short-lived Lease per request key, instead of the per-process requestCache sync.Map.
type distributedDedup struct {
	clientset k8sclient.Interface
	namespace string
	identity  string
	ttl       time.Duration
}

// newDistributedDedup is the injectable constructor used by tests (fake clientset).
func newDistributedDedup(namespace, identity string, clientset k8sclient.Interface, ttl time.Duration) *distributedDedup {
	return &distributedDedup{clientset: clientset, namespace: namespace, identity: identity, ttl: ttl}
}

// newInClusterDistributedDedup builds the dedup backend using in-cluster config, sharing the same
// clientset/identity construction as the podMonitor Lease.
func newInClusterDistributedDedup(namespace string, ttl time.Duration) (*distributedDedup, error) {
	clientset, identity, err := newInClusterKubeClientAndIdentity()
	if err != nil {
		return nil, err
	}
	return newDistributedDedup(namespace, identity, clientset, ttl), nil
}

// leaseNameForKey maps an arbitrary request key (e.g. "CreateVolume:<name>") to a valid
// Kubernetes object name, since raw keys contain characters (colons, mixed case) that aren't
// allowed in Lease names.
func leaseNameForKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	name := fmt.Sprintf("%s%x", dedupLeaseNamePrefix, sum)
	if len(name) > dedupLeaseNameMaxLen {
		name = name[:dedupLeaseNameMaxLen]
	}
	return name
}

// isLeaseStale reports whether lease is old enough to be safely stolen/reaped.
func isLeaseStale(lease *coordinationv1.Lease, ttl time.Duration) bool {
	ref := lease.Spec.RenewTime
	if ref == nil {
		ref = lease.Spec.AcquireTime
	}
	if ref == nil {
		return true
	}
	return time.Since(ref.Time) > ttl
}

// TryAcquire creates (or steals a stale) Lease for key. Returns errDuplicateInFlight if a
// non-stale Lease already exists (i.e. a genuine in-flight duplicate), or a wrapped error on
// unexpected API failures.
func (d *distributedDedup) TryAcquire(ctx context.Context, key string) error {
	name := leaseNameForKey(key)
	now := metav1.NowMicro()
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: d.namespace,
			Labels:    map[string]string{dedupLeaseLabelKey: dedupLeaseLabelValue},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &d.identity,
			AcquireTime:          &now,
			RenewTime:            &now,
			LeaseDurationSeconds: int32Ptr(int32(d.ttl.Seconds())),
		},
	}

	_, err := d.clientset.CoordinationV1().Leases(d.namespace).Create(ctx, lease, metav1.CreateOptions{})
	if err == nil {
		log.Infof("dedup: acquired lease %s for key '%s' (identity=%s, namespace=%s)", name, key, d.identity, d.namespace)
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create dedup lease %s: %w", name, err)
	}

	// Already exists: steal it if stale, otherwise it's a genuine in-flight duplicate.
	existing, getErr := d.clientset.CoordinationV1().Leases(d.namespace).Get(ctx, name, metav1.GetOptions{})
	if getErr != nil {
		return fmt.Errorf("failed to get existing dedup lease %s: %w", name, getErr)
	}
	if !isLeaseStale(existing, d.ttl) {
		holder := ""
		if existing.Spec.HolderIdentity != nil {
			holder = *existing.Spec.HolderIdentity
		}
		log.Infof("dedup: duplicate request detected for key '%s', lease %s held by %s (namespace=%s)", key, name, holder, d.namespace)
		return errDuplicateInFlight
	}

	previousHolder := ""
	if existing.Spec.HolderIdentity != nil {
		previousHolder = *existing.Spec.HolderIdentity
	}
	existing.Spec.HolderIdentity = &d.identity
	existing.Spec.AcquireTime = &now
	existing.Spec.RenewTime = &now
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[dedupLeaseLabelKey] = dedupLeaseLabelValue
	// Update uses existing's resourceVersion (from the Get above) for optimistic concurrency: a
	// Conflict here means a racing replica won the steal first.
	if _, updateErr := d.clientset.CoordinationV1().Leases(d.namespace).Update(ctx, existing, metav1.UpdateOptions{}); updateErr != nil {
		if apierrors.IsConflict(updateErr) {
			log.Infof("dedup: lost race stealing stale lease %s for key '%s' to another replica (identity=%s, namespace=%s)", name, key, d.identity, d.namespace)
			return errDuplicateInFlight
		}
		return fmt.Errorf("failed to steal stale dedup lease %s: %w", name, updateErr)
	}
	log.Infof("dedup: stole stale lease %s for key '%s' from previous holder %s (identity=%s, namespace=%s)", name, key, previousHolder, d.identity, d.namespace)
	return nil
}

// Release deletes the Lease for key, tolerating NotFound (idempotent, matches ClearRequest's
// existing tolerant error style).
func (d *distributedDedup) Release(ctx context.Context, key string) error {
	name := leaseNameForKey(key)
	err := d.clientset.CoordinationV1().Leases(d.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	log.Infof("dedup: released lease %s for key '%s' (identity=%s, namespace=%s)", name, key, d.identity, d.namespace)
	return nil
}

// runReaper periodically deletes stale dedup Leases — housekeeping for keys that are never
// retried (e.g. a process killed between TryAcquire succeeding and ClearRequest's deferred
// Release running). Not required for correctness: TryAcquire's steal-on-acquire already
// guarantees a retry for the same key is never permanently blocked.
func (d *distributedDedup) runReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reapOnce(ctx)
		}
	}
}

func (d *distributedDedup) reapOnce(ctx context.Context) {
	leases, err := d.clientset.CoordinationV1().Leases(d.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", dedupLeaseLabelKey, dedupLeaseLabelValue),
	})
	if err != nil {
		log.Errorf("dedup reaper: failed to list leases: %s", err.Error())
		return
	}
	reaped := 0
	for i := range leases.Items {
		lease := &leases.Items[i]
		if !isLeaseStale(lease, d.ttl) {
			continue
		}
		if err := d.clientset.CoordinationV1().Leases(d.namespace).Delete(ctx, lease.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			log.Errorf("dedup reaper: failed to delete stale lease %s: %s", lease.Name, err.Error())
			continue
		}
		reaped++
	}
	if reaped > 0 {
		log.Infof("dedup reaper: deleted %d stale dedup lease(s)", reaped)
	}
}

func int32Ptr(v int32) *int32 { return &v }
