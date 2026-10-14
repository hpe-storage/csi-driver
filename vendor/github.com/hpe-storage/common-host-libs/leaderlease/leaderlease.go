// Copyright 2026 Hewlett Packard Enterprise Development LP

// Package leaderlease wraps a single Kubernetes-Lease-backed leader-elected role (e.g. "serve
// this process's main responsibility", "run this background monitor loop"), correctly handling
// client-go's tools/leaderelection.LeaderElector.Run being one-shot (acquire -> renew ->
// return-on-loss, never re-contests on its own).
//
// Two policies are supported via Config.ExitOnUnexpectedLoss:
//   - true:  exit the process on an unexpected Lease loss (the Primera CSP's pattern - correct
//     when the process's only job is this role, so a kubelet restart cleanly rejoins the
//     election from scratch).
//   - false: stay in-process and re-contest with a freshly-built elector after a short jittered
//     backoff (needed when the process also has other responsibilities that must not be killed
//     just to recover this one role - e.g. csi-driver's pod monitor, which shares a process with
//     the CSI gRPC server).
//
// Both policies share the same Lease lifecycle plumbing (callbacks, graceful-shutdown draining,
// env-configurable timings via distributedlock.GetEnvDuration) so the mechanism is fixed and
// tested once instead of hand-copied per caller.
package leaderlease

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/hpe-storage/common-host-libs/distributedlock"
	log "github.com/hpe-storage/common-host-libs/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Config describes a single Lease-backed leader-elected role.
type Config struct {
	LeaseName string
	Namespace string

	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration

	// OnStartedLeading is called when this instance acquires the Lease. May be nil.
	OnStartedLeading func(ctx context.Context)
	// OnStoppedLeading is called every time this instance stops leading, whether gracefully
	// (Shutdown) or not. wasLeader distinguishes a real demotion from a standby that never won
	// the Lease, so callers can log/act accordingly. May be nil.
	OnStoppedLeading func(wasLeader bool)

	// ExitOnUnexpectedLoss selects the recovery policy on an unexpected Lease loss - see the
	// package doc comment. Defaults to false (re-contest in-process) if unset.
	ExitOnUnexpectedLoss bool

	// Exit is called with code 1 when ExitOnUnexpectedLoss is true and the Lease is lost
	// unexpectedly. Defaults to os.Exit; injectable for tests.
	Exit func(code int)
}

// Elector runs a Config's leader election for as long as Start's background loop is running.
type Elector struct {
	cfg       Config
	clientset k8sclient.Interface
	identity  string

	isLeader     atomic.Bool
	shuttingDown atomic.Bool
	cancel       context.CancelFunc
	done         chan struct{}

	// runCtx is the context passed to Start, set once before the first Run and read only from
	// within callbacks invoked synchronously by Run on the same goroutine - see wrappedCallbacks.
	runCtx context.Context
}

// New builds an Elector using the given clientset/identity - injectable so tests can pass a fake
// clientset without needing a real cluster.
func New(clientset k8sclient.Interface, identity string, cfg Config) (*Elector, error) {
	if cfg.LeaseName == "" || cfg.Namespace == "" {
		return nil, fmt.Errorf("leaderlease: LeaseName and Namespace are required")
	}
	if cfg.Exit == nil {
		cfg.Exit = os.Exit
	}
	return &Elector{cfg: cfg, clientset: clientset, identity: identity, done: make(chan struct{})}, nil
}

// NewInCluster builds an Elector using in-cluster config and the pod's hostname as identity.
func NewInCluster(cfg Config) (*Elector, error) {
	clientset, identity, err := distributedlock.NewInClusterKubeClientAndIdentity()
	if err != nil {
		return nil, err
	}
	return New(clientset, identity, cfg)
}

// IsLeader reports whether this instance currently holds the Lease.
func (e *Elector) IsLeader() bool { return e.isLeader.Load() }

// Start builds the first elector synchronously (a config-validation failure here is a permanent
// programming/config error - fail closed immediately, matching this codebase's existing
// fail-closed convention for optional Lease-gated features), then runs the election loop in the
// background until ctx is cancelled or Shutdown is called.
func (e *Elector) Start(ctx context.Context) error {
	le, err := e.buildElector()
	if err != nil {
		return fmt.Errorf("leaderlease: failed to build elector for %s: %w", e.cfg.LeaseName, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	go e.runLoop(ctx, le)
	return nil
}

// Shutdown drains in-flight work via drain(), then releases the Lease and waits up to
// cfg.RenewDeadline for the election loop to finish - release() is itself internally bounded by
// exactly that duration, so it's a tight, correct bound rather than a guessed constant.
func (e *Elector) Shutdown(drain func()) {
	drain()

	e.shuttingDown.Store(true)
	if e.cancel != nil {
		e.cancel()
	}

	select {
	case <-e.done:
		log.Infof("leaderlease: %s election stopped cleanly", e.cfg.LeaseName)
	case <-time.After(e.cfg.RenewDeadline):
		log.Warnf("leaderlease: timed out after %s waiting for %s election to stop; proceeding with shutdown", e.cfg.RenewDeadline, e.cfg.LeaseName)
	}
}

// runLoop blocks on Run with the first (already-built) elector, then - only when
// ExitOnUnexpectedLoss is false and the loss wasn't due to ctx cancellation - rebuilds a fresh
// elector and re-contests after a short jittered backoff, repeating until ctx is done.
func (e *Elector) runLoop(ctx context.Context, first *leaderelection.LeaderElector) {
	defer close(e.done)
	// Set once, read by wrappedCallbacks' OnStoppedLeading on this same goroutine (only ever
	// invoked synchronously from within le.Run below) - see the runCtx field doc comment.
	e.runCtx = ctx
	le := first
	for {
		le.Run(ctx) // blocks until ctx is cancelled or this instance loses the lease unexpectedly
		if ctx.Err() != nil {
			return // graceful shutdown already handled by the wrapped OnStoppedLeading
		}
		if e.cfg.ExitOnUnexpectedLoss {
			return // OnStoppedLeading already called cfg.Exit(1)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait.Jitter(2*time.Second, 1.0)):
		}

		var err error
		le, err = e.buildElector()
		if err != nil {
			// Same Config as the first (successful) build - this would only happen if Config
			// were mutated after Start, which callers must not do. Treat as fatal to the loop
			// rather than spinning forever on a deterministic failure.
			log.Errorf("leaderlease: failed to rebuild elector for %s, giving up: %s", e.cfg.LeaseName, err.Error())
			return
		}
	}
}

func (e *Elector) buildElector() (*leaderelection.LeaderElector, error) {
	return leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      e.cfg.LeaseName,
				Namespace: e.cfg.Namespace,
			},
			Client: e.clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: e.identity,
			},
		},
		LeaseDuration: e.cfg.LeaseDuration,
		RenewDeadline: e.cfg.RenewDeadline,
		RetryPeriod:   e.cfg.RetryPeriod,
		// Always on: release immediately on ctx cancellation (graceful shutdown) rather than
		// leaving a standby to wait out the full LeaseDuration. Strictly beneficial (faster
		// failover) regardless of ExitOnUnexpectedLoss, so it isn't a separate Config knob.
		ReleaseOnCancel: true,
		Callbacks:       e.wrappedCallbacks(),
	})
}

func (e *Elector) wrappedCallbacks() leaderelection.LeaderCallbacks {
	return leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			log.Infof("leaderlease: acquired %s lease; running as leader", e.cfg.LeaseName)
			e.isLeader.Store(true)
			if e.cfg.OnStartedLeading != nil {
				e.cfg.OnStartedLeading(ctx)
			}
		},
		OnStoppedLeading: func() {
			// wasLeader distinguishes a real demotion from a standby that never won the Lease,
			// so the graceful-shutdown log line below doesn't claim a release that never happened.
			wasLeader := e.isLeader.Swap(false)
			if e.cfg.OnStoppedLeading != nil {
				e.cfg.OnStoppedLeading(wasLeader)
			}
			// e.runCtx.Err() is the same signal runLoop itself uses to decide whether to
			// re-contest, so this can never disagree with runLoop even if a future caller
			// cancels the context passed to Start directly instead of calling Shutdown.
			graceful := e.shuttingDown.Load() || e.runCtx.Err() != nil
			if graceful {
				if wasLeader {
					log.Infof("leaderlease: released %s lease (graceful stop)", e.cfg.LeaseName)
				} else {
					log.Infof("leaderlease: stopped contesting %s lease (graceful stop)", e.cfg.LeaseName)
				}
				return
			}
			if e.cfg.ExitOnUnexpectedLoss {
				log.Errorf("leaderlease: lost %s lease unexpectedly; exiting to rejoin election", e.cfg.LeaseName)
				e.cfg.Exit(1)
				return
			}
			log.Warnf("leaderlease: lost %s lease unexpectedly; will re-contest", e.cfg.LeaseName)
		},
	}
}
