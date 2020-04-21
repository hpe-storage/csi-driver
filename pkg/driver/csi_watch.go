// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	notify "github.com/fsnotify/fsnotify"
	log "github.com/hpe-storage/common-host-libs/logger"
)

type CSIWatch struct {
	// Channel to receive the stop event.
	watchStop chan struct{}
	// fsnotify watcher.
	watchList *notify.Watcher
	// Anonymous function.
	watchRun func()
	// Wait
	wg sync.WaitGroup
}

// InitializeWatcher is used to initialized CSIWatch with anonymous function and new watcher.
// It regulary monitors os signals like SIGTERM,SIGHUP etc in a separate thread for
// graceful exit of watcher.

func (driver *Driver) InitializeWatcher(job func()) (*CSIWatch, error) {
	log.Trace(">>>>> InitializeWatcher")
	defer log.Trace("<<<<< InitializeWatcher")
	watcher, err := notify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Initialization
	task := &CSIWatch{
		watchStop: make(chan struct{}),
		watchList: watcher,
		watchRun:  job,
	}
	task.wg.Add(1)

	// Create a channel for OS signal
	sigc := make(chan os.Signal)
	// List of os signals to monitor.
	signal.Notify(sigc,
		syscall.SIGABRT,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGKILL,
	)
	// Create a thread to monitor the os signals.
	go func() {
		select {
		case sig := <-sigc:
			log.Infof("Received %s os signal. Exiting...\n", sig)
			// Call stopWatcher() for gracefull exit of watcher.
			task.stopWatcher()
			task.wg.Wait()
		}
	}()

	return task, nil
}

//AddWatchList list of files /and direcror to watch
func (t *CSIWatch) AddWatchList(files []string) error {
	log.Trace(">>>>> AddWatchList")
	defer log.Trace("<<<<< AddWatchList")
	if len(files) == 0 {
		return fmt.Errorf("Error empty watch list, there should be at least one file to watch")
	}
	for _, fPath := range files {

		err := t.watchList.Add(fPath)
		if err != nil {
			log.Warnf("Failed add [%s] file to watch list, err :", fPath, err.Error())
		} else {
			log.Tracef("Successfully added [%s] file to watch list", fPath)
		}

	}
	return nil
}

// Start polling until os sig intterupt. This will run anonymous fn forever.
func (t *CSIWatch) StartWatcher() {
	log.Trace(">>>>> StartWatcher")
	defer log.Trace("<<<<< StartWatcher")
	pid := os.Getpid()
	log.Tracef("CSI watcher [%d PID] successfull started", pid)
	// forever
	for {
		select {
		case <-t.watchStop:
			log.Infof("Stopping [%d PID ] csi watcher", pid)
			t.wg.Done()
			t.watchList.Close()
			return
		case <-t.watchList.Events:
			log.Infof("CSI Watcher [%d PID], received notification", pid)
			t.watchRun()
			log.Infof("CSI Wacther [%d PID], notification served", pid)
		}
	}
}

// This is used internally to stop the watcher.
func (t *CSIWatch) stopWatcher() {
	log.Trace(">>>>> stopWatcher")
	defer log.Trace("<<<<< stopWatcher")
	close(t.watchStop)
}
