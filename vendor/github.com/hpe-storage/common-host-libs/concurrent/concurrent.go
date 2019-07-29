/*
(c) Copyright 2018 Hewlett Packard Enterprise Development LP
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package concurrent

import (
	"sync"

	log "github.com/hpe-storage/common-host-libs/logger"
)

type mLock struct {
	mutex *sync.Mutex
	count int
}

//MapMutex locks and blocks on specific keys
type MapMutex struct {
	lockMap map[string]*mLock
	bigLock *sync.Mutex
}

//NewMapMutex creates a new MapMutex
func NewMapMutex() *MapMutex {
	return &MapMutex{
		lockMap: make(map[string]*mLock),
		bigLock: &sync.Mutex{},
	}
}

//Lock creates and locks or blocks on a lock
func (m *MapMutex) Lock(lockName string) {
	log.Trace("Acquiring mutex lock for ", lockName)

	m.bigLock.Lock()
	// do defer unlock here, unlock at specific locations

	if littleLock, present := m.lockMap[lockName]; present {
		littleLock.count++
		// release the big lock because we need to sit behind the one from the map
		m.bigLock.Unlock()
		// wait until we get the lock
		littleLock.mutex.Lock()
		return
	}

	// no lock, create one and lock it
	m.lockMap[lockName] = &mLock{mutex: &sync.Mutex{}, count: 1}
	m.lockMap[lockName].mutex.Lock()
	// release the big lock
	m.bigLock.Unlock()
}

// Unlock releases a lock
func (m *MapMutex) Unlock(lockName string) {
	log.Trace("Releasing mutex lock for ", lockName)

	m.bigLock.Lock()
	defer m.bigLock.Unlock()

	if littleLock, present := m.lockMap[lockName]; present {
		littleLock.count--
		if littleLock.count == 0 {
			delete(m.lockMap, lockName)
		}
		littleLock.mutex.Unlock()
	}
}
