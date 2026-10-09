// Copyright 2026 Hewlett Packard Enterprise Development LP

// Package distributedlock provides cross-pod duplicate-request dedup via one Kubernetes Lease per
// request key. Shared by csi-driver and csi-extensions (CON-4982/CON-4983) so the engine is fixed
// and tested in one place instead of hand-copied and drifting between the two repos. Callers own
// their own HandleDuplicateRequest/ClearRequest orchestration (e.g. precedence against an
// in-process map or an etcd-backed DBService); this package only implements the Lease tier.
package distributedlock

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	dedupLeaseNamePrefix = "hpe-csi-dedup-"
	dedupLeaseLabelKey   = "storage.hpe.com/dedup-lock"
	dedupLeaseLabelValue = "true"
	dedupLeaseNameMaxLen = 253
)

// Default timings, overridable via env vars read through GetEnvDuration by each caller (e.g.
// DEDUP_LOCK_TTL/DEDUP_REAPER_INTERVAL/DEDUP_MAX_HOLD_DURATION). DefaultReaperInterval defaults
// much longer than DefaultLockTTL — it's pure housekeeping for rare, never-retried orphans
// (TryAcquire's steal-on-acquire alone guarantees correctness), so it runs infrequently to
// minimize List calls (every controller replica runs its own reaper).
const (
	DefaultLockTTL         = 60 * time.Second
	DefaultReaperInterval  = 15 * time.Minute
	DefaultMaxHoldDuration = 5 * time.Minute
)

// dedupLeaseCallTimeout bounds every Lease API call this package makes (TryAcquire, Release, the
// reaper, and the background renewal loop) so a slow/wedged apiserver can never block a caller -
// or leak the renewal goroutine - forever. Callers only ever get cancellation from their own ctx
// otherwise, which isn't a deadline.
const dedupLeaseCallTimeout = 10 * time.Second

// dedupAcquireRetryBackoff governs how TryAcquire retries after a transient (non-permanent) API
// failure - e.g. a brief apiserver network blip - before giving up. ErrDuplicateInFlight and
// permanent errors (see isPermanentLeaseError) are never retried: they're the correct, final
// answer, not a symptom of a blip. 3 attempts total, 1s/2s backoff between them; respects ctx's
// own cancellation/deadline too, so a caller with a tighter budget is honored.
var dedupAcquireRetryBackoff = wait.Backoff{
	Duration: 1 * time.Second,
	Factor:   2.0,
	Steps:    3,
}

// ErrDuplicateInFlight is returned by TryAcquire when a non-stale Lease for the key already exists.
var ErrDuplicateInFlight = errors.New("duplicate request already in flight")

// DistributedDedup gates duplicate in-flight requests across controller replicas using a
// short-lived Lease per request key. Held Leases are kept alive by a per-key renewal goroutine
// (see startRenewal) for as long as the op is in flight, so staleness only ever reflects a dead
// holder, never a merely slow one — bounded by maxHoldDuration, so a wedged holder that never
// calls Release still lets the lease go stale eventually (see renewOnce).
type DistributedDedup struct {
	clientset       k8sclient.Interface
	namespace       string
	identity        string
	ttl             time.Duration
	maxHoldDuration time.Duration

	renewMu  sync.Mutex
	renewals map[string]context.CancelFunc // key -> cancel for that key's renewal goroutine
}

// New is the injectable constructor used by tests (fake clientset) and by callers that already
// have their own clientset/identity.
func New(namespace, identity string, clientset k8sclient.Interface, ttl, maxHoldDuration time.Duration) *DistributedDedup {
	return &DistributedDedup{
		clientset:       clientset,
		namespace:       namespace,
		identity:        identity,
		ttl:             ttl,
		maxHoldDuration: maxHoldDuration,
		renewals:        make(map[string]context.CancelFunc),
	}
}

// NewInClusterKubeClientAndIdentity builds an in-cluster Kubernetes clientset plus a holder
// identity (the pod's hostname). Exported so callers that also run their own Lease-based
// mechanism (e.g. a leader-elected pod monitor) can share the same clientset/identity
// construction instead of duplicating it.
func NewInClusterKubeClientAndIdentity() (k8sclient.Interface, string, error) {
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

// NewInCluster builds the dedup backend using in-cluster config.
func NewInCluster(namespace string, ttl, maxHoldDuration time.Duration) (*DistributedDedup, error) {
	clientset, identity, err := NewInClusterKubeClientAndIdentity()
	if err != nil {
		return nil, err
	}
	return New(namespace, identity, clientset, ttl, maxHoldDuration), nil
}

// GetEnvDuration reads a duration from envVarName (e.g. "15s"), falling back to defaultVal if
// unset or invalid. Used for Lease timings (TTL, reaper interval, max hold duration) by dedup
// callers, and reused by leader-election-style callers (e.g. a pod monitor Lease) for their own
// env-configurable timings.
func GetEnvDuration(envVarName string, defaultVal time.Duration) time.Duration {
	if envVal := os.Getenv(envVarName); envVal != "" {
		if parsed, err := time.ParseDuration(envVal); err == nil && parsed > 0 {
			return parsed
		}
		log.Warnf("Invalid %s=%q, using default %s", envVarName, envVal, defaultVal)
	}
	return defaultVal
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

// isPermanentLeaseError reports whether err is unlikely to succeed on retry within the same short
// window (RBAC/misconfiguration/malformed request), as opposed to a transient blip (network
// timeout, apiserver momentarily unavailable) worth retrying quickly.
func isPermanentLeaseError(err error) bool {
	return apierrors.IsForbidden(err) ||
		apierrors.IsUnauthorized(err) ||
		apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) ||
		apierrors.IsMethodNotSupported(err) ||
		apierrors.IsNotAcceptable(err)
}

// TryAcquire creates (or steals a stale) Lease for key, retrying transient API failures (anything
// other than ErrDuplicateInFlight or a permanent error - see isPermanentLeaseError) with bounded
// backoff (dedupAcquireRetryBackoff) instead of failing the caller's RPC on a single blip.
func (d *DistributedDedup) TryAcquire(ctx context.Context, key string) error {
	var lastErr error
	err := wait.ExponentialBackoffWithContext(ctx, dedupAcquireRetryBackoff, func(attemptCtx context.Context) (bool, error) {
		attemptErr := d.tryAcquireOnce(attemptCtx, key)
		if attemptErr == nil {
			return true, nil
		}
		lastErr = attemptErr
		if errors.Is(attemptErr, ErrDuplicateInFlight) || isPermanentLeaseError(attemptErr) {
			return false, attemptErr // final answer, don't retry
		}
		log.Warnf("dedup: transient error acquiring lease for key '%s', will retry: %s", key, attemptErr.Error())
		return false, nil // retryable, keep trying
	})
	if err != nil && wait.Interrupted(err) {
		// Backoff/ctx exhausted without the condition ever returning true or a real error (every
		// attempt was retryable and still failed) - surface the last real error, not wait's generic
		// "timed out waiting for the condition" sentinel.
		if lastErr != nil {
			return lastErr
		}
		return err
	}
	return err
}

// tryAcquireOnce is a single attempt at TryAcquire's create-or-steal-stale logic, bounded by
// dedupLeaseCallTimeout regardless of ctx's own deadline, so a wedged apiserver can't block a
// single attempt indefinitely.
func (d *DistributedDedup) tryAcquireOnce(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, dedupLeaseCallTimeout)
	defer cancel()

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
		d.startRenewal(key, name)
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
		return ErrDuplicateInFlight
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
			return ErrDuplicateInFlight
		}
		return fmt.Errorf("failed to steal stale dedup lease %s: %w", name, updateErr)
	}
	log.Infof("dedup: stole stale lease %s for key '%s' from previous holder %s (identity=%s, namespace=%s)", name, key, previousHolder, d.identity, d.namespace)
	d.startRenewal(key, name)
	return nil
}

// Release stops this key's renewal goroutine, then deletes the Lease - but only if this instance
// still holds it (an unowned Lease belongs to whoever legitimately holds/stole it now, and must
// never be deleted out from under them). Bounded by dedupLeaseCallTimeout regardless of ctx's own
// deadline, so a wedged apiserver can't block the calling RPC's deferred cleanup indefinitely.
func (d *DistributedDedup) Release(ctx context.Context, key string) error {
	d.stopRenewal(key)

	ctx, cancel := context.WithTimeout(ctx, dedupLeaseCallTimeout)
	defer cancel()

	name := leaseNameForKey(key)
	existing, err := d.clientset.CoordinationV1().Leases(d.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Infof("dedup: lease %s for key '%s' already gone (identity=%s, namespace=%s)", name, key, d.identity, d.namespace)
			return nil
		}
		return fmt.Errorf("failed to get dedup lease %s before release: %w", name, err)
	}
	if existing.Spec.HolderIdentity == nil || *existing.Spec.HolderIdentity != d.identity {
		log.Warnf("dedup: lease %s for key '%s' is no longer held by this instance, skipping delete (identity=%s, namespace=%s)", name, key, d.identity, d.namespace)
		return nil
	}

	// Preconditions guard the Get-then-Delete gap: if another replica stole the Lease in between,
	// Delete fails with Conflict instead of removing the new legitimate holder's lock.
	deleteErr := d.clientset.CoordinationV1().Leases(d.namespace).Delete(ctx, name, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: &existing.UID, ResourceVersion: &existing.ResourceVersion},
	})
	if deleteErr != nil && !apierrors.IsNotFound(deleteErr) && !apierrors.IsConflict(deleteErr) {
		return deleteErr
	}
	log.Infof("dedup: released lease %s for key '%s' (identity=%s, namespace=%s)", name, key, d.identity, d.namespace)
	return nil
}

// startRenewal starts a background goroutine that periodically bumps RenewTime on the Lease
// named name for key, so a legitimately slow-but-alive op never looks stale to another replica.
// Only called right after TryAcquire creates or steals the Lease.
func (d *DistributedDedup) startRenewal(key, name string) {
	ctx, cancel := context.WithCancel(context.Background())
	d.renewMu.Lock()
	d.renewals[key] = cancel
	d.renewMu.Unlock()

	// ttl/3 mirrors etcd concurrency.Session's own keepalive cadence, giving two missed renewals
	// of slack before a holder goes stale.
	interval := d.ttl / 3
	if interval <= 0 {
		interval = time.Second
	}
	go d.runRenewal(ctx, key, name, interval)
}

// stopRenewal cancels and forgets key's renewal goroutine. Safe to call even if none was ever
// started for key.
func (d *DistributedDedup) stopRenewal(key string) {
	d.renewMu.Lock()
	cancel, ok := d.renewals[key]
	if ok {
		delete(d.renewals, key)
	}
	d.renewMu.Unlock()
	if ok {
		cancel()
	}
}

// runRenewal ticks every interval, renewing the Lease until ctx is cancelled (via stopRenewal) or
// renewOnce reports this instance no longer holds the Lease.
func (d *DistributedDedup) runRenewal(ctx context.Context, key, name string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !d.renewOnce(ctx, key, name) {
				return
			}
		}
	}
}

// renewOnce bumps RenewTime on the Lease named name if this instance still holds it. Returns
// false to stop the renewal loop (no longer the owner, or the Lease is gone), true to retry on
// the next tick (transient errors).
func (d *DistributedDedup) renewOnce(ctx context.Context, key, name string) bool {
	if ctx.Err() != nil {
		return false
	}

	getCtx, cancel := context.WithTimeout(ctx, dedupLeaseCallTimeout)
	defer cancel()
	existing, err := d.clientset.CoordinationV1().Leases(d.namespace).Get(getCtx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Infof("dedup: lease %s for key '%s' no longer exists, stopping renewal (identity=%s)", name, key, d.identity)
			return false
		}
		log.Warnf("dedup: failed to get lease %s for key '%s' to renew, will retry: %s", name, key, err.Error())
		return true
	}
	if existing.Spec.HolderIdentity == nil || *existing.Spec.HolderIdentity != d.identity {
		log.Warnf("dedup: lease %s for key '%s' is now held by another replica, stopping renewal (identity=%s)", name, key, d.identity)
		return false
	}

	// Hard ceiling, independent of ctx/ownership: a handler goroutine that never returns (and so
	// never calls Release) would otherwise renew forever, permanently blocking every future retry
	// for this key. AcquireTime (set once by TryAcquire, never bumped by renewal) gives the true
	// total hold time; nil can't happen in practice but is treated as "stop" defensively rather
	// than assumed fresh.
	acquired := existing.Spec.AcquireTime
	if acquired == nil || time.Since(acquired.Time) > d.maxHoldDuration {
		log.Warnf("dedup: lease %s for key '%s' held past max %s, letting it go stale (identity=%s)", name, key, d.maxHoldDuration, d.identity)
		return false
	}

	now := metav1.NowMicro()
	existing.Spec.RenewTime = &now
	updateCtx, cancel := context.WithTimeout(ctx, dedupLeaseCallTimeout)
	defer cancel()
	if _, updateErr := d.clientset.CoordinationV1().Leases(d.namespace).Update(updateCtx, existing, metav1.UpdateOptions{}); updateErr != nil {
		if apierrors.IsConflict(updateErr) {
			log.Warnf("dedup: lost lease %s for key '%s' to another replica during renew, stopping renewal (identity=%s)", name, key, d.identity)
			return false
		}
		if apierrors.IsNotFound(updateErr) {
			log.Infof("dedup: lease %s for key '%s' deleted during renew, stopping renewal (identity=%s)", name, key, d.identity)
			return false
		}
		log.Warnf("dedup: failed to renew lease %s for key '%s', will retry: %s", name, key, updateErr.Error())
		return true
	}
	log.Tracef("dedup: renewed lease %s for key '%s' (identity=%s)", name, key, d.identity)
	return true
}

// RunReaper periodically deletes stale dedup Leases — housekeeping for keys that are never
// retried (e.g. a process killed between TryAcquire succeeding and Release never running). Not
// required for correctness: TryAcquire's steal-on-acquire already guarantees a retry for the same
// key is never permanently blocked.
func (d *DistributedDedup) RunReaper(ctx context.Context, interval time.Duration) {
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

func (d *DistributedDedup) reapOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, dedupLeaseCallTimeout)
	defer cancel()

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
		// Preconditions guard the List-then-Delete gap: if another replica legitimately stole
		// this Lease (TryAcquire's steal-on-acquire) in between, Delete fails with Conflict
		// instead of deleting the new holder's lock out from under it (the same race Release's
		// owner-check guards against).
		deleteErr := d.clientset.CoordinationV1().Leases(d.namespace).Delete(ctx, lease.Name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &lease.UID, ResourceVersion: &lease.ResourceVersion},
		})
		if deleteErr != nil && !apierrors.IsNotFound(deleteErr) && !apierrors.IsConflict(deleteErr) {
			log.Errorf("dedup reaper: failed to delete stale lease %s: %s", lease.Name, deleteErr.Error())
			continue
		}
		if deleteErr != nil {
			continue // NotFound or Conflict: already gone or stolen since List - not an error.
		}
		reaped++
	}
	if reaped > 0 {
		log.Infof("dedup reaper: deleted %d stale dedup lease(s)", reaped)
	}
}

func int32Ptr(v int32) *int32 { return &v }
