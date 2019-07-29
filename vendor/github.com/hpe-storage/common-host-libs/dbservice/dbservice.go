// Copyright 2019 Hewlett Packard Enterprise Development LP

package dbservice

import "github.com/Scalingo/go-etcd-lock/lock"

// DBService defines the interface to any DB related operations
type DBService interface {
	// TODO: Add TLS support
	Get(key string) (*string, error)
	Put(key string, value string) error
	PutWithLeaseExpiry(key string, value string, seconds int64) error
	Delete(key string) error
	IsLocked(key string) (bool, error)
	AcquireLock(key string, ttl int) (lock.Lock, error)
	WaitAcquireLock(key string, ttl int) (lock.Lock, error)
	ReleaseLock(lck lock.Lock) error
	// TODO: Add more operations
}
