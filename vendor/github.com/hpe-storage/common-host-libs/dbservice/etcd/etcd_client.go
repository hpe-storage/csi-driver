// Copyright 2019 Hewlett Packard Enterprise Development LP

package etcd

import (
	"context"
	"fmt"
	"time"

	"github.com/Scalingo/go-etcd-lock/lock"
	v3 "github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/etcdserver/api/v3rpc/rpctypes"

	"github.com/hpe-storage/common-host-libs/jsonutil"
	log "github.com/hpe-storage/common-host-libs/logger"
)

var (
	// DefaultVersion of etcd DB
	DefaultVersion = "3"

	// DefaultHost IP address
	DefaultHost = "127.0.0.1"

	// DefaultPort number for client
	DefaultPort = "2379"

	// DefaultDialTimeout in seconds
	DefaultDialTimeout = 30 * time.Second

	// DefaultRequestTimeout in seconds
	DefaultRequestTimeout = 5 * time.Second
)

// DBClient is an implementor of the DBService interface
type DBClient struct {
	Version   string
	EndPoints []string
	Client    *v3.Client
}

// NewClient creates new client instance for list of endpoints to access the DB server
func NewClient(endPoints []string, version string) (*DBClient, error) {
	if version == DefaultVersion {
		// V3 client
		return NewClientV3(endPoints)
	}
	return nil, fmt.Errorf("DB service client with version %s is not supported", version)
}

// NewClientV3 creates v3 client instance to access the DB server
func NewClientV3(endPoints []string) (*DBClient, error) {
	log.Tracef("NewClientV3, endpoints: %v", endPoints)

	cli, err := v3.New(v3.Config{
		Endpoints:   endPoints,
		DialTimeout: DefaultDialTimeout,
		// TODO: Add TLS support for security, may need to generate certs
	})
	if err != nil {
		// handle error!
		log.Error("Failed to create etcd v3 client, err: ", err.Error())
		return nil, err
	}

	return &DBClient{
		Version:   DefaultVersion,
		EndPoints: endPoints,
		Client:    cli,
	}, nil
}

// CloseClient closes the DB client
func (d *DBClient) CloseClient() error {
	return d.Client.Close()
}

// IsLocked checks if the given key is already locked
func (d *DBClient) IsLocked(key string) (bool, error) {
	log.Tracef(">>>>> IsLocked, key: %s", key)
	defer log.Trace("<<<<< IsLocked")

	locker := lock.NewEtcdLocker(d.Client)
	lck, err := locker.Acquire(key, 1)
	if err != nil {
		// Check if it's lock err object
		if lockErr, ok := err.(*lock.Error); ok {
			log.Tracef("[Key: %s], %s", key, lockErr)
			return true, nil // Key already in 'locked' state
		}
		// Communication with etcd has failed or other error
		log.Error(err.Error())
		return false, err
	}
	// Release the acquired lock here
	lck.Release()

	return false, nil
}

// AcquireLock for the given key
func (d *DBClient) AcquireLock(key string, ttl int) (lock.Lock, error) {
	log.Tracef(">>>>> AcquireLock, key: %s, ttl: %d", key, ttl)
	defer log.Trace("<<<<< AcquireLock")

	locker := lock.NewEtcdLocker(d.Client)
	lck, err := locker.Acquire(key, ttl)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	return lck, nil
}

// WaitAcquireLock for the given key
func (d *DBClient) WaitAcquireLock(key string, ttl int) (lock.Lock, error) {
	log.Tracef(">>>>> WaitAcquireLock, key: %s, ttl: %d", key, ttl)
	defer log.Trace("<<<<< WaitAcquireLock")

	locker := lock.NewEtcdLocker(d.Client)
	lck, err := locker.WaitAcquire(key, ttl)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	jsonutil.PrintPrettyJSONToConsole(locker)
	return lck, nil
}

// ReleaseLock for the given key
func (d *DBClient) ReleaseLock(lck lock.Lock) error {
	log.Tracef(">>>>> ReleaseLock, %#v", lck)
	defer log.Trace("<<<<< ReleaseLock")

	if err := lck.Release(); err != nil {
		// Something wrong can happen during release: connection problem with etcd
		log.Errorf("Failed to unlock the key, err: %v", err.Error())
		return err
	}
	return nil
}

// Get value from DB
func (d *DBClient) Get(key string) (*string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultRequestTimeout)
	resp, err := d.Client.Get(ctx, key)
	cancel()
	if err != nil {
		log.Errorf("GET %s failed, err: %s", key, err.Error())
		return nil, handleError(err)
	}
	log.Tracef("GET %s success, metadata: %v", key, resp)
	if resp.Kvs != nil {
		log.Tracef("%s : %s\n", resp.Kvs[0].Key, resp.Kvs[0].Value)
		value := string(resp.Kvs[0].Value)
		return &value, nil
	}
	// Key not found and No error
	return nil, nil
}

// Put value to DB
func (d *DBClient) Put(key string, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultRequestTimeout)
	resp, err := d.Client.Put(ctx, key, value)
	cancel()
	if err != nil {
		log.Errorf("PUT %s failed, err: %s", key, err.Error())
		return handleError(err)
	}
	log.Tracef("PUT %s success, metadata: %v", key, resp)
	return nil
}

// PutWithLeaseExpiry value to DB
func (d *DBClient) PutWithLeaseExpiry(key string, value string, seconds int64) error {

	// Minimum lease TTL is 5-second
	if seconds < 5 {
		return fmt.Errorf("Minimum lease TTL is 5 seconds")
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultRequestTimeout)
	// Get lease
	leaseResp, err := d.Client.Grant(ctx, seconds)
	if err != nil {
		log.Errorf("PUT %s failed, err: %s", key, err.Error())
		cancel()
		return err
	}
	resp, err := d.Client.Put(ctx, key, value, v3.WithLease(leaseResp.ID))
	cancel()
	if err != nil {
		log.Errorf("PUT %s failed, err: %s", key, err.Error())
		return handleError(err)
	}
	log.Tracef("PUT %s success, metadata: %v", key, resp)
	return nil
}

// Delete value from DB
func (d *DBClient) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultRequestTimeout)
	resp, err := d.Client.Delete(ctx, key)
	cancel()
	if err != nil {
		log.Errorf("DELETE %s failed, err: %s", key, err.Error())
		return handleError(err)
	}
	log.Tracef("DELETE %s success, metadata: %v", key, resp)
	return nil
}

func handleError(err error) error {
	switch err {
	case context.Canceled:
		return fmt.Errorf("ctx is canceled by another routine: %v", err)
	case context.DeadlineExceeded:
		return fmt.Errorf("ctx is attached with a deadline is exceeded: %v", err)
	case rpctypes.ErrEmptyKey:
		return fmt.Errorf("client-side error: %v", err)
	default:
		return fmt.Errorf("bad cluster endpoints, which are not etcd servers: %v", err)
	}
}

// Get value for the given key
func Get(key string) (*string, error) {
	// Create DB Client
	endPoints := []string{fmt.Sprintf("%s:%s", DefaultHost, DefaultPort)}
	dbClient, err := NewClient(endPoints, DefaultVersion)
	if err != nil {
		return nil, err
	}
	defer dbClient.CloseClient()
	return dbClient.Get(key)
}

// Put value for the given key and value
func Put(key string, value string) error {
	// Create DB Client
	endPoints := []string{fmt.Sprintf("%s:%s", DefaultHost, DefaultPort)}
	dbClient, err := NewClient(endPoints, DefaultVersion)
	if err != nil {
		return err
	}
	defer dbClient.CloseClient()
	return dbClient.Put(key, value)
}

// Delete the given key
func Delete(key string) error {
	// Create DB Client
	endPoints := []string{fmt.Sprintf("%s:%s", DefaultHost, DefaultPort)}
	dbClient, err := NewClient(endPoints, DefaultVersion)
	if err != nil {
		return err
	}
	defer dbClient.CloseClient()
	return dbClient.Delete(key)
}
