/*
(c) Copyright 2017 Hewlett Packard Enterprise Development LP

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

package util

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
)

const (
	defaultTimeout = 60
)

func execCommandOutputWithTimeout(cmd string, args []string, stdinArgs []string, timeout int) (string, int, error) {
	log.Trace("execCommandOutputWithTimeout called with ", cmd, log.Scrubber(args), timeout)
	var err error
	c := exec.Command(cmd, args...)
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = &b
	if len(stdinArgs) > 0 {
		c.Stdin = strings.NewReader(strings.Join(stdinArgs, "\n"))
	}
	if err = c.Start(); err != nil {
		return "", 999, err
	}

	// Wait for the process to finish or kill it after a timeout:
	done := make(chan error, 1)
	go func() {
		done <- c.Wait()
	}()
	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		if err = c.Process.Kill(); err != nil {
			log.Errorf("failed to kill process %v: error =  %v", c.Process.Pid, err)
		}
		err = fmt.Errorf("command %s with pid: %v killed as timeout of %d seconds reached", cmd, c.Process.Pid, timeout)
		log.Errorf(err.Error())
	case err = <-done:
		if err != nil {
			log.Errorf("process with pid : %v finished with error = %v", c.Process.Pid, err)
		} else {
			log.Tracef("process with pid: %v finished successfully", c.Process.Pid)
		}
	}
	out := string(b.Bytes())
	log.Trace(out)
	if err != nil {
		//check the rc of the exec
		if badnews, ok := err.(*exec.ExitError); ok {
			if status, ok := badnews.Sys().(syscall.WaitStatus); ok {
				// send the error code and stderr content to the caller
				return out, status.ExitStatus(), fmt.Errorf("command %s failed with rc=%d err=%s", cmd, status.ExitStatus(), out)
			}
		} else {
			return out, 888, fmt.Errorf("error %s", err.Error())
		}
	}
	return out, 0, nil
}

// ExecCommandOutputWithTimeout  executes ExecCommandOutput with the specified timeout
func ExecCommandOutputWithTimeout(cmd string, args []string, timeout int) (string, int, error) {
	return execCommandOutputWithTimeout(cmd, args, []string{}, timeout)
}

// ExecCommandOutput returns stdout and stderr in a single string, the return code, and error.
// If the return code is not zero, error will not be nil.
// Stdout and Stderr are dumped to the log at the debug level.
// Return code of 999 indicates an error starting the command.
func ExecCommandOutput(cmd string, args []string) (string, int, error) {
	return ExecCommandOutputWithTimeout(cmd, args, defaultTimeout)
}

// ExecCommandOutputWithStdinArgs returns stdout and stderr in a single string, the return code, and error.
// If the return code is not zero, error will not be nil.
// Stdout and Stderr are dumped to the log at the debug level.
// Return code of 999 indicates an error starting the command.
func ExecCommandOutputWithStdinArgs(cmd string, args []string, stdInArgs []string) (string, int, error) {
	return execCommandOutputWithTimeout(cmd, args, stdInArgs, defaultTimeout)
}

// FindStringSubmatchMap : find and build  the map of named groups
func FindStringSubmatchMap(s string, r *regexp.Regexp) map[string]string {
	captures := make(map[string]string)
	match := r.FindStringSubmatch(s)
	if match == nil {
		return captures
	}
	for i, name := range r.SubexpNames() {
		if i != 0 {
			captures[name] = match[i]
		}
	}
	return captures
}
