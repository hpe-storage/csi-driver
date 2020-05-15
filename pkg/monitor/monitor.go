// Copyright 2020 Hewlett Packard Enterprise Development LP

package monitor

import (
	"fmt"
	"os"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
)

const (
	defaultIntervalSec          = 30
	minimumIntervalSec          = 15
	defaultPodMonitorLabelKey   = "monitored-by"
	defaultPodMonitorLabelValue = "hpe-csi"
)

func NewMonitor(flavor flavor.Flavor, monitorInterval int64) *Monitor {
	m := &Monitor{flavor: flavor, intervalSec: monitorInterval, podLabelKey: defaultPodMonitorLabelKey, podLabelValue: defaultPodMonitorLabelValue}
	if key := os.Getenv("MONITOR_POD_LABEL_KEY"); key != "" {
		m.podLabelKey = key
	}
	if value := os.Getenv("MONITOR_POD_LABEL_VALUE"); value != "" {
		m.podLabelValue = value
	}
	// initialize pod monitor
	return m
}

// Monitor Pods running on un-reachable nodes
type Monitor struct {
	flavor        flavor.Flavor
	intervalSec   int64
	lock          sync.Mutex
	started       bool
	stopChannel   chan int
	done          chan int
	podLabelKey   string
	podLabelValue string
}

// StartMonitor starts the monitor
func (m *Monitor) StartMonitor() error {
	log.Trace(">>>>> StartMonitor")
	defer log.Trace("<<<<< StartMonitor")

	m.lock.Lock()
	defer m.lock.Unlock()

	if m.started {
		return fmt.Errorf("Pod monitor has already been started")
	}

	if m.intervalSec == 0 {
		m.intervalSec = defaultIntervalSec
	} else if m.intervalSec < minimumIntervalSec {
		log.Warnf("minimum interval for health monitor is %v seconds", minimumIntervalSec)
		m.intervalSec = minimumIntervalSec
	}

	m.stopChannel = make(chan int)
	m.done = make(chan int)

	if err := m.monitorPod(); err != nil {
		return err
	}

	m.started = true
	return nil
}

// StopMonitor stops the monitor
func (m *Monitor) StopMonitor() error {
	log.Trace(">>>>> StopMonitor")
	defer log.Trace("<<<<< StopMonitor")

	m.lock.Lock()
	defer m.lock.Unlock()

	if !m.started {
		return fmt.Errorf("Pod monitor has not been started")
	}

	close(m.stopChannel)
	<-m.done

	m.started = false
	return nil
}

func (m *Monitor) monitorPod() error {
	log.Trace(">>>>> monitorPod")
	defer log.Trace("<<<<< monitorPod")
	defer close(m.done)

	tick := time.NewTicker(time.Duration(m.intervalSec) * time.Second)

	go func() {
		for {
			select {
			case <-tick.C:
				err := m.flavor.MonitorPod(m.podLabelKey, m.podLabelValue)
				if err != nil {
					log.Errorf("pod monitoring failed with error %s", err.Error())
				}
			case <-m.stopChannel:
				return
			}
		}
	}()
	return nil
}
