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

package linux

import (
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
)

const (
	selinuxenabled = "selinuxenabled"
	chcon          = "chcon"
)

//SelinuxEnabled runs selinuxenabled if found and returns the result.  If its not found, false is returned.
// From man - It exits with status 0 if SELinux is enabled and 1 if it is not enabled.
func SelinuxEnabled() bool {
	_, rc, err := util.ExecCommandOutput(selinuxenabled, nil)
	log.Tracef("selinuxenabled returned %d and err=%v", rc, err)
	if rc == 0 && err == nil {
		return true
	}
	log.Trace("selinux is NOT enabled")
	return false
}

//Chcon - chcon -t svirt_sandbox_file_t <mount point>
func Chcon(context, path string) error {
	if SelinuxEnabled() {
		log.Tracef("Chcon about to change context of %s to %s", path, context)
		args := []string{"-t", context, path}
		_, _, err := util.ExecCommandOutput(chcon, args)
		if err != nil {
			return err
		}
	}
	return nil
}
