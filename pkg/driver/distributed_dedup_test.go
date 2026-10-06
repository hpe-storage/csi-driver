// Copyright 2026 Hewlett Packard Enterprise Development LP

package driver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"

	"k8s.io/client-go/kubernetes/fake"
)

const testDedupNamespace = "hpe-storage"

func TestTryAcquireRelease(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	d := newDistributedDedup(testDedupNamespace, "pod-a", clientset, time.Minute)
	key := "CreateVolume:pvc-1"

	assert.NoError(t, d.TryAcquire(context.Background(), key))

	// Second acquire on the same key, before release, must be reported as a duplicate.
	err := d.TryAcquire(context.Background(), key)
	assert.ErrorIs(t, err, errDuplicateInFlight)

	assert.NoError(t, d.Release(context.Background(), key))

	// After release, the key can be acquired again.
	assert.NoError(t, d.TryAcquire(context.Background(), key))
}

func TestTryAcquireStealsStaleLease(t *testing.T) {
	key := "ControllerExpandVolume:vol-1"
	name := leaseNameForKey(key)
	old := metav1.NewTime(time.Now().Add(-time.Hour))
	holder := "pod-crashed"
	ttl := int32(60)
	staleLease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testDedupNamespace,
			Labels:    map[string]string{dedupLeaseLabelKey: dedupLeaseLabelValue},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			AcquireTime:          &metav1.MicroTime{Time: old.Time},
			RenewTime:            &metav1.MicroTime{Time: old.Time},
			LeaseDurationSeconds: &ttl,
		},
	}
	clientset := fake.NewSimpleClientset(staleLease)
	d := newDistributedDedup(testDedupNamespace, "pod-b", clientset, 60*time.Second)

	assert.NoError(t, d.TryAcquire(context.Background(), key), "a stale lease should be stolen, not reported as a duplicate")
}

func TestTryAcquireConflictOnRace(t *testing.T) {
	key := "CreateSnapshot:snap-1:vol-1"
	name := leaseNameForKey(key)
	old := metav1.NewTime(time.Now().Add(-time.Hour))
	holder := "pod-crashed"
	ttl := int32(60)
	staleLease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testDedupNamespace,
			Labels:    map[string]string{dedupLeaseLabelKey: dedupLeaseLabelValue},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			AcquireTime:          &metav1.MicroTime{Time: old.Time},
			RenewTime:            &metav1.MicroTime{Time: old.Time},
			LeaseDurationSeconds: &ttl,
		},
	}
	clientset := fake.NewSimpleClientset(staleLease)
	// Force the next Update() on this Lease to fail with Conflict, simulating a racing replica
	// winning the steal first.
	clientset.PrependReactor("update", "leases", func(action kubetesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(coordinationv1.Resource("leases"), name, assert.AnError)
	})
	d := newDistributedDedup(testDedupNamespace, "pod-b", clientset, 60*time.Second)

	err := d.TryAcquire(context.Background(), key)
	assert.ErrorIs(t, err, errDuplicateInFlight, "a Conflict on steal must be treated as a duplicate, not an internal error")
}

func TestReaperDeletesOnlyStaleLabeledLeases(t *testing.T) {
	old := metav1.NewTime(time.Now().Add(-time.Hour))
	recent := metav1.NewTime(time.Now())
	holder := "pod-x"
	ttl := int32(60)

	staleLabeled := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "hpe-csi-dedup-stale", Namespace: testDedupNamespace, Labels: map[string]string{dedupLeaseLabelKey: dedupLeaseLabelValue}},
		Spec:       coordinationv1.LeaseSpec{HolderIdentity: &holder, RenewTime: &metav1.MicroTime{Time: old.Time}, LeaseDurationSeconds: &ttl},
	}
	freshLabeled := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "hpe-csi-dedup-fresh", Namespace: testDedupNamespace, Labels: map[string]string{dedupLeaseLabelKey: dedupLeaseLabelValue}},
		Spec:       coordinationv1.LeaseSpec{HolderIdentity: &holder, RenewTime: &metav1.MicroTime{Time: recent.Time}, LeaseDurationSeconds: &ttl},
	}
	unrelated := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "hpe-csi-driver-podmonitor-leader", Namespace: testDedupNamespace},
		Spec:       coordinationv1.LeaseSpec{HolderIdentity: &holder, RenewTime: &metav1.MicroTime{Time: old.Time}, LeaseDurationSeconds: &ttl},
	}

	clientset := fake.NewSimpleClientset(staleLabeled, freshLabeled, unrelated)
	d := newDistributedDedup(testDedupNamespace, "pod-x", clientset, 60*time.Second)

	d.reapOnce(context.Background())

	_, err := clientset.CoordinationV1().Leases(testDedupNamespace).Get(context.Background(), "hpe-csi-dedup-stale", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "stale labeled lease should have been reaped")

	_, err = clientset.CoordinationV1().Leases(testDedupNamespace).Get(context.Background(), "hpe-csi-dedup-fresh", metav1.GetOptions{})
	assert.NoError(t, err, "fresh labeled lease must not be reaped")

	_, err = clientset.CoordinationV1().Leases(testDedupNamespace).Get(context.Background(), "hpe-csi-driver-podmonitor-leader", metav1.GetOptions{})
	assert.NoError(t, err, "unrelated unlabeled lease must never be touched by the reaper")
}

func TestHandleDuplicateRequestAndClearRequestDelegateToDistributedDedup(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	d := &Driver{distributedDedup: newDistributedDedup(testDedupNamespace, "pod-a", clientset, time.Minute)}
	key := "DeleteVolume:vol-1"

	assert.NoError(t, d.HandleDuplicateRequest(key))
	err := d.HandleDuplicateRequest(key)
	assert.Error(t, err, "a second HandleDuplicateRequest for the same key must be reported as ABORTED")

	d.ClearRequest(key)
	assert.NoError(t, d.HandleDuplicateRequest(key), "after ClearRequest, the key should be acquirable again")
}
