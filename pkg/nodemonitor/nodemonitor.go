// Copyright 2020 Hewlett Packard Enterprise Development LP

package nodemonitor

import (
	"fmt"
	"os"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/nodeinit"
)

const (
	defaultIntervalSec = 30
	minimumIntervalSec = 15
)

func NewNodeMonitor(flavor flavor.Flavor, monitorInterval int64) *NodeMonitor {
	nm := &NodeMonitor{flavor: flavor, intervalSec: monitorInterval}
	if key := os.Getenv("NODE_NAME"); key != "" {
		nm.nodeName = key
	}
	log.Infof("NODE MONITOR: %+v", nm)
	// initialize node monitor
	return nm
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
func (nm *NodeMonitor) StartNodeMonitor() error {
	log.Trace(">>>>> StartNodeMonitor")
	defer log.Trace("<<<<< StartNodeMonitor")

	nm.lock.Lock()
	defer nm.lock.Unlock()

	if nm.started {
		return fmt.Errorf("Node monitor has already been started")
	}

	if nm.intervalSec == 0 {
		nm.intervalSec = defaultIntervalSec
	} else if nm.intervalSec < minimumIntervalSec {
		log.Warnf("minimum interval for health monitor is %v seconds", minimumIntervalSec)
		nm.intervalSec = minimumIntervalSec
	}

	nm.stopChannel = make(chan int)
	nm.done = make(chan int)

	if err := nm.monitorNode(); err != nil {
		return err
	}

	nm.started = true
	return nil
}

// StopMonitor stops the monitor
func (nm *NodeMonitor) StopNodeMonitor() error {
	log.Trace(">>>>> StopNodeMonitor")
	defer log.Trace("<<<<< StopNodeMonitor")

	nm.lock.Lock()
	defer nm.lock.Unlock()

	if !nm.started {
		return fmt.Errorf("Node monitor has not been started")
	}

	close(nm.stopChannel)
	<-nm.done

	nm.started = false
	return nil
}

func (nm *NodeMonitor) monitorNode() error {
	log.Trace(">>>>> monitorNode for the node ", nm.nodeName)
	defer log.Trace("<<<<< monitorNode")
	defer close(nm.done)

	tick := time.NewTicker(time.Duration(nm.intervalSec) * time.Second)

	go func() {
		for {
			select {
			case <-tick.C:
				log.Infof("Node monitor started monitoring the node %s", nm.nodeName)
				err := nodeinit.AnalyzeMultiPathDevices(nm.flavor, nm.nodeName)
				if err != nil {
					log.Errorf("Error while analyzing the multipath devices %s on the node %s", err.Error(), nm.nodeName)
					return
				}
			case <-nm.stopChannel:
				return
			}
		}
	}()
	return nil
}
