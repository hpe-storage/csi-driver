// Copyright 2019 Hewlett Packard Enterprise Development LP

package driver

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
)

type DriverTask struct {
	// Channel to receive the stop event.
	taskStop chan struct{}
	// Poll delay.
	taskTick *time.Ticker
	// Anonymous function for execution.
	taskExecute func()
	// Wait for poll job.
	wg sync.WaitGroup
}

// InitTask is used to initialized DriverTask with anonymous function and polling interval.
// It regulary monitors os signals like SIGTERM,SIGHUP etc in a separate thread for
// graceful exit of polling job.

func (driver *Driver) InitTask(job func(), delay time.Duration) *DriverTask {
	log.Trace(">>>>> InitTask")
	defer log.Trace("<<<<< InitTask")

	// Initialization
	task := &DriverTask{
		taskStop:    make(chan struct{}),
		taskTick:    time.NewTicker(time.Minute * delay),
		taskExecute: job,
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
			// Call stop() for gracefull exit of poll routine.
			task.stop()
			task.wg.Wait()
		}
	}()

	return task
}

// Start polling until os sig intterupt. This will run anonymous fn forever.
func (t *DriverTask) Start() {
	log.Trace(">>>>> Start")
	defer log.Trace("<<<<< Start")
	pid := os.Getpid()
	// forever
	for {
		select {
		case <-t.taskStop:
			log.Infof("Stopping task, pid [%d]", pid)
			t.wg.Done()
			return
		case <-t.taskTick.C:
			log.Infof("Start task, pid %d.", pid)
			t.taskExecute()
			log.Infof("Completed %d pid task", pid)
		}
	}
}

// This is used internally to stop polling.
func (t *DriverTask) stop() {
	log.Trace(">>>>> stop")
	defer log.Trace("<<<<< stop")
	close(t.taskStop)
}
