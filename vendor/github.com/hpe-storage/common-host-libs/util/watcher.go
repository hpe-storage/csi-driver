// Copyright 2020 Hewlett Packard Enterprise Development LP

package util

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	notify "github.com/fsnotify/fsnotify"
	log "github.com/hpe-storage/common-host-libs/logger"
)
const tickerDefaultDelay = 60 // Min
// FileWatch contains watcher attributes.
type FileWatch struct {
	// Channel to receive the stop event.
	watchStop chan struct{}
	// fsnotify watcher.
	watchList *notify.Watcher
	// Anonymous function.
	watchRun func()
	// Wait
	wg sync.WaitGroup
}

// InitializeWatcher is used to initialize fileWatch with anonymous function and new watcher.
// It regularly monitors os signals like SIGTERM,SIGHUP etc in a separate thread for
// graceful exit of the watcher.
func InitializeWatcher(job func()) (*FileWatch, error) {
	log.Trace(">>>>> InitializeWatcher")
	defer log.Trace("<<<<< InitializeWatcher")
	watcher, err := notify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Initialization
	watch := &FileWatch{
		watchStop: make(chan struct{}),
		watchList: watcher,
		watchRun:  job,
	}
	watch.wg.Add(1)

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
			// Call stopWatcher() for graceful exit of watcher.
			watch.stopWatcher()
			watch.wg.Wait()
		}
	}()

	return watch, nil
}

//AddWatchList list of files /and directories to watch
func (w *FileWatch) AddWatchList(files []string) error {
	log.Trace(">>>>> AddWatchList")
	defer log.Trace("<<<<< AddWatchList")

	if len(files) == 0 {
		return fmt.Errorf("Empty watch list is not supported, there should be at least one file to watch")
	}

	for _, fPath := range files {
		err := w.watchList.Add(fPath)
		if err != nil {
			log.Warnf("Failed to add [%s] file to watch list, err %s :", fPath, err.Error())
		} else {
			log.Tracef("Successfully added [%s] file to watch list", fPath)
		}
	}
	return nil
}

// StartWatcher triggers watcher until os sig interrupt. This will run anonymous fn forever.
func (w *FileWatch) StartWatcher() {
	log.Trace(">>>>> StartWatcher")
	defer log.Trace("<<<<< StartWatcher")
	pid := os.Getpid()
	log.Tracef("Watcher [%d PID] is successfully started", pid)

	// Control the ticker interval, dont want to frequently wakeup
	// watcher as it is only needed when there is event notification. So if there is
	// event notification, ticker is set to wake up every one minute otherwise sleep
	// for 1 hour.
	var delayControlFlag time.Duration = tickerDefaultDelay

	// This is used to control the flow of events, we dont want to process frequent update
	// If there are multiple update within 1 min, only process one event and ignore the rest of the events
	isSpuriousUpdate:=false
	// forever
	for {
		select {
		case <-w.watchStop:
			log.Infof("Stopping [%d PID ] csi watcher", pid)
			w.wg.Done()
			w.watchList.Close()
			return
		case <-w.watchList.Events:
			// There might be spurious update, ignore the event if it occurs within 1 min.
			if !isSpuriousUpdate {
				log.Infof("Watcher [%d PID], received notification", pid)
				w.watchRun()
				log.Infof("Watcher [%d PID], notification served", pid)
				isSpuriousUpdate = true
				delayControlFlag = 1
			} else {
				log.Warnf("Watcher [%d PID], received spurious notification, ignore", pid)
			}
		case <-time.NewTicker(time.Minute * delayControlFlag).C:
			isSpuriousUpdate = false
			delayControlFlag = tickerDefaultDelay
		}
	}
}

// This is used internally to stop the watcher.
func (w *FileWatch) stopWatcher() {
	log.Trace(">>>>> stopWatcher")
	defer log.Trace("<<<<< stopWatcher")
	close(w.watchStop)
}