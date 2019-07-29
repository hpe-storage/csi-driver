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

package linux

const (
	systemReleaseFile       = "/etc/system-release"
	systemIssueFile         = "/etc/issue"
	systemRedhatReleaseFile = "/etc/redhat-release"
	osReleaseFile           = "/etc/os-release"
	uekKernel               = "uek"
	systemdPath             = "/lib/systemd/system/"
	suseReleasePattern      = "(?P<major>\\d+)\\s+SP(?P<minor>\\d+)"
	systemReleasePattern    = "^\\s*(?P<distro>\\w+).*?(?P<major>\\d+)\\.*(?P<minor>\\d*)"
	lsbReleasePattern       = "\\s*(?P<distro>\\w+)\\s*(?P<major>\\d+)\\.*(?P<minor>\\d*)"
	multipath               = "multipath"
	install                 = "install"
	check                   = "check"
	dpkg                    = "dpkg"
	rpm                     = "rpm"
	yum                     = "yum"
	aptget                  = "apt-get"
	zypper                  = "zypper"
	iscsiInitiatorUtils     = "iscsi-initiator-utils"
	deviceMapperMultipath   = "device-mapper-multipath"
	multipathTools          = "multipath-tools"
	openIscsi               = "open-iscsi"
	iscsid                  = "iscsid"
	multipathd              = "multipathd"
	systemCtl               = "systemctl"
	service                 = "service"
	linuxExtraImage         = "linux-image-extra" // ubuntu package linux-image-extra needed for scsi_dh_alua
)
