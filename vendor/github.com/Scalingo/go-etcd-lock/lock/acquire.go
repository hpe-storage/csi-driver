package lock

import (
	"context"
	"fmt"
	"sync"
	"time"

	etcd "github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/concurrency"
)

type Error struct{}

func (e *Error) Error() string {
	return fmt.Sprintf("key is already locked")
}

type Locker interface {
	Acquire(key string, ttl int) (Lock, error)
	WaitAcquire(key string, ttl int) (Lock, error)
	Wait(key string) error
}

type EtcdLocker struct {
	client *etcd.Client
}

func NewEtcdLocker(client *etcd.Client) Locker {
	return &EtcdLocker{client: client}
}

type Lock interface {
	Release() error
}

type EtcdLock struct {
	*sync.Mutex
	mutex    *concurrency.Mutex
	released bool
}

func (locker *EtcdLocker) Acquire(key string, ttl int) (Lock, error) {
	return locker.acquire(key, ttl, false)
}

func (locker *EtcdLocker) WaitAcquire(key string, ttl int) (Lock, error) {
	return locker.acquire(key, ttl, true)
}

func (locker *EtcdLocker) acquire(key string, ttl int, wait bool) (Lock, error) {
	session, err := concurrency.NewSession(locker.client, concurrency.WithTTL(ttl))
	if err != nil {
		return nil, err
	}
	key = addPrefix(key)
	mutex := concurrency.NewMutex(session, key)

	ctx := context.Background()
	if !wait {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	err = mutex.Lock(ctx)
	if err == context.DeadlineExceeded {
		return nil, &Error{}
	}

	lock := &EtcdLock{mutex: mutex, Mutex: &sync.Mutex{}}

	// Release the lock after the end of the TTL automatically
	go func() {
		select {
		case <-time.After(time.Duration(ttl) * time.Second):
			lock.Release()
		}
	}()

	return lock, nil
}
