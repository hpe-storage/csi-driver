// Copyright 2020 Hewlett Packard Enterprise Development LP

package nodemonitor

import (
	"fmt"
	"os"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/tunelinux"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
)

const (
	defaultIntervalSec = 30
	minimumIntervalSec = 15
)

func NewNodeMonitor(flavor flavor.Flavor, monitorInterval int64) *NodeMonitor {
	m := &NodeMonitor{flavor: flavor, intervalSec: monitorInterval}
	if key := os.Getenv("NODE_NAME"); key != "" {
		m.nodeName = key
	}
	log.Infof("NODE MONITOR: %+v", m)
	// initialize node monitor
	return m
}

// Monitor Pods running on un-reachable nodes
type NodeMonitor struct {
	flavor      flavor.Flavor
	intervalSec int64
	lock        sync.Mutex
	started     bool
	stopChannel chan int
	done        chan int
	nodeName    string
}

// StartMonitor starts the monitor
func (m *NodeMonitor) StartNodeMonitor() error {
	log.Trace(">>>>> StartNodeMonitor")
	defer log.Trace("<<<<< StartNodeMonitor")

	m.lock.Lock()
	defer m.lock.Unlock()

	if m.started {
		return fmt.Errorf("Node monitor has already been started")
	}

	if m.intervalSec == 0 {
		m.intervalSec = defaultIntervalSec
	} else if m.intervalSec < minimumIntervalSec {
		log.Warnf("minimum interval for health monitor is %v seconds", minimumIntervalSec)
		m.intervalSec = minimumIntervalSec
	}

	m.stopChannel = make(chan int)
	m.done = make(chan int)

	if err := m.monitorNode(); err != nil {
		return err
	}

	m.started = true
	return nil
}

// StopMonitor stops the monitor
func (m *NodeMonitor) StopNodeMonitor() error {
	log.Trace(">>>>> StopNodeMonitor")
	defer log.Trace("<<<<< StopNodeMonitor")

	m.lock.Lock()
	defer m.lock.Unlock()

	if !m.started {
		return fmt.Errorf("Node monitor has not been started")
	}

	close(m.stopChannel)
	<-m.done

	m.started = false
	return nil
}

func (m *NodeMonitor) monitorNode() error {
	log.Trace(">>>>> monitorNode")
	defer log.Trace("<<<<< monitorNode")
	defer close(m.done)

	tick := time.NewTicker(time.Duration(m.intervalSec) * time.Second)

	go func() {
		for {
			select {
			case <-tick.C:
				log.Infof("NODE MONITOR :Monitoring node......1")
				multipathDevices, err := tunelinux.GetMultipathDevices() //driver.GetMultipathDevices()
				if err != nil {
					log.Errorf("Error while getting the multipath devices")
					return
				}
				if multipathDevices != nil && len(multipathDevices) > 0 {
					unhealthyDevices, err := tunelinux.GetUnhealthyMultipathDevices(multipathDevices)
					if err != nil {
						log.Errorf("Error while retreiving unhealthy multipath devices: %s", err.Error())
					}
					log.Tracef("Unhealthy multipath devices found are: %+v", unhealthyDevices)
					if len(unhealthyDevices) > 0 {
						log.Tracef("Unhealthy multipath devices found on the node %s", m.nodeName)
						//Do cleanup
					} else {
						log.Tracef("No unhealthy multipath devices found on the node %s", m.nodeName)
						//check whether they belong to this node or not
					}
				} else {
					log.Tracef("No multipath devices found on the node %s", m.nodeName)
				}
				log.Infof("NODE MONITOR :Monitoring node......2")
			case <-m.stopChannel:
				return
			}
		}
	}()
	return nil
}
