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

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
)

const (
	// OsTypeRedhat represents RedHat OS
	OsTypeRedhat = "redhat"
	// OsTypeCentos represents CentOS OS
	OsTypeCentos = "centos"
	// OsTypeOracle represents Oracle Linux OS
	OsTypeOracle = "oracle"
	// OsTypeUbuntu represents Ubuntu OS
	OsTypeUbuntu = "ubuntu"
	// OsTypeSuse represents SuSE OS
	OsTypeSuse = "suse"
	// OsTypeAmazon represents Amazon OS
	OsTypeAmazon = "amazon"
)

var (
	lsbReleaseRegexp    = regexp.MustCompile(lsbReleasePattern)
	systemReleaseRegexp = regexp.MustCompile(systemReleasePattern)
	suseReleaseRegexp   = regexp.MustCompile(suseReleasePattern)
)

// OsMultipathPackageMap provides mapping of os distribution to multipath package name
var OsMultipathPackageMap = map[string]string{
	OsTypeUbuntu: multipathTools,
	OsTypeSuse:   multipathTools,
	OsTypeRedhat: deviceMapperMultipath,
	OsTypeCentos: deviceMapperMultipath,
	OsTypeOracle: deviceMapperMultipath,
	OsTypeAmazon: deviceMapperMultipath,
}

// OsIscsiPackageMap provides mapping of os distribution to iscsi package name
var OsIscsiPackageMap = map[string]string{
	OsTypeUbuntu: openIscsi,
	OsTypeSuse:   openIscsi,
	OsTypeRedhat: iscsiInitiatorUtils,
	OsTypeCentos: iscsiInitiatorUtils,
	OsTypeOracle: iscsiInitiatorUtils,
	OsTypeAmazon: iscsiInitiatorUtils,
}

var osInfo *OsInfo
var osInfoLock sync.Mutex

// OsInfo represents OS information
type OsInfo struct {
	osDistro       string
	osMajorVersion string
	osMinorVersion string
	kernelVersion  string
}

// GetOsInfo returns os information including distro and versions
func GetOsInfo() (*OsInfo, error) {
	osInfoLock.Lock()
	defer osInfoLock.Unlock()
	var err error
	if osInfo == nil {
		osInfo, err = setOsInfo()
		if err != nil {
			log.Errorln("unable to set os info ", err.Error())
			return nil, err
		}
		log.Infof("got OS details as [%s %s %s %s]\n", osInfo.GetOsDistro(), osInfo.GetOsMajorVersion(), osInfo.GetOsMinorVersion(), osInfo.GetKernelVersion())
	}
	return osInfo, nil
}

func GetDistro() (string, error) {
	osInfoLock.Lock()
	defer osInfoLock.Unlock()
	var out, distro string
	if f, err := os.Stat(osReleaseFile); err == nil && !f.IsDir() && f.Size() != 0 {
		// get only PRETTY_NAME field of os-release
		var lines []string
		lines, err = util.FileGetStrings(osReleaseFile)
		for _, line := range lines {
			if strings.Contains(line, "PRETTY_NAME") {
				// remove quotes and key
				out = strings.Replace(strings.Replace(line, "\"", "", -1), "PRETTY_NAME=", "", -1)
				break
			}
		}

		if out != "" {
			if strings.Contains(out, "Ubuntu") {
				distro = "Ubuntu"
			} else if strings.Contains(out, "Red Hat") {
				distro = "Red Hat"
			} else if strings.Contains(out, "Centos") {
				distro = "Centos"
			} else if strings.Contains(out, "SUSE") {
				distro = "SUSE"
			} else {
				log.Info("Cannot determine Distro")
				return "", errors.New("Undefined DistroType")
			}
			return distro, nil
		}
	}
	return "", errors.New("Unable to determine DistroType")

}

// GetKernelVersion returns OS kernel version
func (o *OsInfo) GetKernelVersion() string {
	return strings.TrimSpace(o.kernelVersion)
}

// GetOsDistro will return OS distribution
func (o *OsInfo) GetOsDistro() string {
	return strings.TrimSpace(o.osDistro)
}

// GetOsMajorVersion returns OS major version
func (o *OsInfo) GetOsMajorVersion() string {
	return o.osMajorVersion
}

// GetOsMinorVersion returns OS minor version
func (o *OsInfo) GetOsMinorVersion() string {
	return o.osMinorVersion
}

// GetOsVersion returns OS release version in major.minor format
func (o *OsInfo) GetOsVersion() string {
	return fmt.Sprintf("%s.%s", o.osMajorVersion, o.osMinorVersion)
}

// IsSystemdSupported returns true if systmed is used as service manager on the system
func (o *OsInfo) IsSystemdSupported() bool {
	args := []string{"--version"}
	_, rc, err := util.ExecCommandOutput("systemctl", args)
	if err != nil || rc != 0 {
		log.Traceln("systemd is not available on the system")
		return false
	}
	return true
}

// IsUekKernel returns true if Oracle Linux is running with UEK kernel
func (o *OsInfo) IsUekKernel() bool {
	return o.GetKernelVersion() != "" && strings.Contains(strings.ToLower(o.GetKernelVersion()), uekKernel)
}

func isLsbReleaseAvailable() bool {
	args := []string{"-a"}
	_, rc, err := util.ExecCommandOutput("lsb_release", args)
	if err != nil || rc != 0 {
		log.Traceln("lsb_release is not available on the system")
		return false
	}
	return true
}

func obtainOsInfoUsingLsbRelease() (string, error) {
	args := []string{"-si", "-sr"}
	out, _, err := util.ExecCommandOutput("lsb_release", args)
	if err != nil {
		return "", err
	}
	return out, nil
}

// From container, system files can appear as directories, ignore them
// https://github.com/kubernetes/kubernetes/issues/65825
// nolint :
func setOsInfo() (osInfo *OsInfo, err error) {
	var out string
	isObtainedUsingLsbRelease := false
	// try to fetch os info using lsb_release if available
	if isLsbReleaseAvailable() {
		out, err = obtainOsInfoUsingLsbRelease()
		if err != nil {
			return nil, err
		}
		isObtainedUsingLsbRelease = true
	} else if f, err := os.Stat(systemReleaseFile); err == nil && !f.IsDir() && f.Size() != 0 {
		out, err = util.FileReadFirstLine(systemReleaseFile)
	} else if f, err := os.Stat(systemRedhatReleaseFile); err == nil && !f.IsDir() && f.Size() != 0 {
		out, err = util.FileReadFirstLine(systemRedhatReleaseFile)
	} else if f, err := os.Stat(osReleaseFile); err == nil && !f.IsDir() && f.Size() != 0 {
		// get only PRETTY_NAME field of os-release
		var lines []string
		lines, err = util.FileGetStrings(osReleaseFile)
		for _, line := range lines {
			if strings.Contains(line, "PRETTY_NAME") {
				// remove quotes and key
				out = strings.Replace(strings.Replace(line, "\"", "", -1), "PRETTY_NAME=", "", -1)
				break
			}
		}
	} else if f, err := os.Stat(systemIssueFile); err == nil && !f.IsDir() && f.Size() != 0 {
		// /etc/issue contains empty lines, so get first non-empty line
		var lines []string
		lines, err = util.FileGetStrings(systemIssueFile)
		for _, line := range lines {
			if len(strings.TrimSpace(line)) > 0 {
				out = line
				break
			}
		}
	} else {
		err = errors.New("unable to obtain OS info as none of the system release info files are found, please install lsb_release package and rerun the command")
	}

	if err != nil {
		return nil, err
	}

	if out != "" {
		osInfo, err = populateOsInfo(out, isObtainedUsingLsbRelease)
		if err != nil {
			log.Debugf("Unable to populate Os Info %s", err.Error())
			return nil, err
		}
	}
	return osInfo, nil
}

func populateOsInfo(output string, isObtainedUsingLsbRelease bool) (osInfo *OsInfo, err error) {
	osInfo = &OsInfo{}
	var r *regexp.Regexp
	if strings.Contains(output, "SUSE") {
		// SLES 15 doesn't have SP versions yet, so set minor version as 0
		if strings.Contains(output, "15") && !strings.Contains(output, "SP") {
			osInfo.osDistro = OsTypeSuse
			osInfo.osMajorVersion = "15"
			osInfo.osMinorVersion = "0"
			return osInfo, nil
		}
		if isObtainedUsingLsbRelease {
			r = lsbReleaseRegexp
		} else {
			r = suseReleaseRegexp
		}
		// suse has a special pattern so set the type here and rest we depend on regex
		osInfo.osDistro = OsTypeSuse
	} else if isObtainedUsingLsbRelease {
		r = lsbReleaseRegexp
	} else {
		r = systemReleaseRegexp
	}

	// now parse os type and version
	result := util.FindStringSubmatchMap(output, r)
	if len(result) == 0 {
		return osInfo, fmt.Errorf("unable to match os info (%v) with pattern (%v)", output, r.String())
	}

	log.Tracef("matched os info map:%s", result)
	osInfo.setOsDistro(result)
	osInfo.setOsMajorVersion(result)
	osInfo.setOsMinorVersion(result)
	osInfo.setKernelVersion()
	return osInfo, nil
}

func (o *OsInfo) setOsDistro(result map[string]string) {
	if _, ok := result["distro"]; ok {
		if strings.Contains(result["distro"], "Red") {
			o.osDistro = OsTypeRedhat
		} else if strings.Contains(result["distro"], "CentOS") {
			o.osDistro = OsTypeCentos
		} else if strings.Contains(result["distro"], "Ubuntu") {
			o.osDistro = OsTypeUbuntu
		} else if strings.Contains(result["distro"], "Oracle") {
			o.osDistro = OsTypeOracle
		} else if strings.Contains(result["distro"], "Amazon") {
			o.osDistro = OsTypeAmazon
		}
	}
}

func (o *OsInfo) setOsMajorVersion(result map[string]string) {
	if _, ok := result["major"]; ok {
		o.osMajorVersion = result["major"]
	}
}

func (o *OsInfo) setOsMinorVersion(result map[string]string) {
	if _, ok := result["minor"]; ok {
		o.osMinorVersion = result["minor"]
	}
}

func (o *OsInfo) setKernelVersion() {
	args := []string{"-r"}
	out, _, err := util.ExecCommandOutput("uname", args)
	if err == nil {
		o.kernelVersion = out
	}
}

// IsPackageInstalled returns true if specified package type is installed on host
func IsPackageInstalled(packageType string) (bool, error) {
	log.Traceln("IsPackageInstalled called with packageType ", packageType)

	if packageType != iscsi && packageType != multipath && packageType != linuxExtraImage {
		return true, errors.New("Only multipath and iscsi package types are supported for install")
	}

	var rc int
	osDetails, err := GetOsInfo()
	if err != nil {
		log.Errorln("unable to get host os information ", err.Error())
		return true, err
	}
	cmd, args := getPackageCommandArgs(osDetails, packageType, check)
	log.Traceln("running command ", cmd, " args: ", args)
	_, rc, err = util.ExecCommandOutput(cmd, args)
	if err != nil || rc != 0 {
		log.Warnf("package %s is not installed on host\n", packageType)
		return false, err
	}
	return true, nil
}

// InstallPackage installs provided package type on the host based on OS distro
func InstallPackage(packageType string) error {
	log.Traceln("InstallPackage called with packageType ", packageType)
	var err error
	var yes bool
	if yes, err = IsPackageInstalled(packageType); yes {
		log.Infof("%s package is already installed on the host\n", packageType)
		if err != nil {
			// just print the error as not being supported
			log.Tracef(err.Error())
		}
		return nil
	}
	if err != nil && !yes {
		// obtain os type info
		osDetails, err := GetOsInfo()
		if err != nil {
			log.Errorln("unable to get host os information ", err.Error())
			return err
		}
		// get suitable command args based on os type, package type and install operation
		cmd, args := getPackageCommandArgs(osDetails, packageType, install)
		log.Traceln("running command ", cmd, " args: ", args)
		_, _, err = util.ExecCommandOutputWithTimeout(cmd, args, 180 /* 3 Min */)
		if err != nil {
			log.Errorf("unable to install %s package %v\n", packageType, err.Error())
			return err
		}
	}
	return nil
}

func applySuseWorkaround() (err error) {
	// apply workaround for SuSE for multipathd.service, NLT-1226
	if _, err = os.Stat("/usr/lib/systemd/system/multipathd.service"); err == nil {
		args := []string{"-i", "s/^ConditionKernelCommandLine=!multipath=off/#ConditionKernelCommandLine=!multipath=off/", "/usr/lib/systemd/system/multipathd.service"}
		_, _, err = util.ExecCommandOutput("sed", args)
		if err != nil {
			log.Errorf("unable to apply suse workaround for multipathd.service package %v", err.Error())
			return err
		}
		args = []string{"-i", "s/^ConditionKernelCommandLine=!nompath/#ConditionKernelCommandLine=!nompath/", "/usr/lib/systemd/system/multipathd.service"}
		_, _, err = util.ExecCommandOutput("sed", args)
		if err != nil {
			log.Errorf("unable to apply suse workaround for multipathd.service package %v", err.Error())
			return err
		}
		args = []string{"-i", "s/multipath=off/multipath=on/", "/boot/grub2/grub.cfg"}
		_, _, err = util.ExecCommandOutput("sed", args)
		if err != nil {
			log.Errorf("unable to apply suse workaround for multipathd.service package %v", err.Error())
			return err
		}
		// reload service with changes
		args = []string{"daemon-reload"}
		_, _, err = util.ExecCommandOutput("systemctl", args)
		if err != nil {
			log.Errorf("unable to reload systemd after multipathd.service changes for sles %v\n", err.Error())
			return err
		}
	}
	return nil
}

func getPackageCommandArgs(osInfo *OsInfo, packageType string, operation string) (cmd string, args []string) {
	switch osInfo.GetOsDistro() {
	case OsTypeRedhat, OsTypeCentos, OsTypeOracle, OsTypeAmazon:
		cmd, args = getRedhatBasedPackageArgs(packageType, operation)
	case OsTypeUbuntu:
		cmd, args = getUbuntuBasedPackageArgs(osInfo, packageType, operation)
	case OsTypeSuse:
		cmd, args = getSuseBasedPackageArgs(packageType, operation)
	}
	return cmd, args
}

func getRedhatBasedPackageArgs(packageType string, operation string) (cmd string, args []string) {
	log.Traceln("Called getRedhatBasedArgs called with packageType:", packageType, "operation:", operation)
	if packageType == iscsi {
		if operation == check {
			args = []string{"-qi", iscsiInitiatorUtils}
			cmd = rpm
		} else if operation == install {
			args = []string{"-y", "install", iscsiInitiatorUtils}
			cmd = yum
		}
	} else if packageType == multipath {
		if operation == check {
			args = []string{"-qi", deviceMapperMultipath}
			cmd = rpm
		} else if operation == install {
			args = []string{"-y", "install", deviceMapperMultipath}
			cmd = yum
		}
	}
	return cmd, args
}

func getUbuntuBasedPackageArgs(osInfo *OsInfo, packageType string, operation string) (cmd string, args []string) {
	log.Traceln("Called getUbuntuBasedPackageArgs called with packageType:", packageType, "operation:", operation)
	if packageType == iscsi {
		if operation == check {
			args = []string{"-l", "|", "grep", "open-iscsi"}
			cmd = dpkg
		} else if operation == install {
			args = []string{"--assume-yes", "install", "open-iscsi"}
			cmd = aptget
		}
	} else if packageType == multipath {
		if operation == check {
			args = []string{"-l", "|", "grep", "multipath-tools"}
			cmd = dpkg
		} else if operation == install {
			args = []string{"--assume-yes", "install", "multipath-tools"}
			cmd = aptget
		}
	} else if packageType == linuxExtraImage {
		if operation == check {
			args = []string{"-l", "|", "grep", "linux-image-extra-`uname -r`"}
			cmd = dpkg
		} else if operation == install {
			args = []string{"--assume-yes", "install", "linux-image-extra-" + osInfo.GetKernelVersion()}
			cmd = aptget
		}
	}
	return cmd, args
}

func getSuseBasedPackageArgs(packageType string, operation string) (cmd string, args []string) {
	log.Traceln("Called getSuseBasedPackageArgs called with packageType:", packageType, "operation:", operation)
	if packageType == iscsi {
		if operation == check {
			args = []string{"-qi", "open-iscsi"}
			cmd = rpm
		} else if operation == install {
			args = []string{"--non-interactive", "install", "open-iscsi"}
			cmd = zypper
		}
	} else if packageType == multipath {
		if operation == check {
			args = []string{"-qi", "multipath-tools"}
			cmd = rpm
		} else if operation == install {
			args = []string{"--non-interactive", "install", "multipath-tools"}
			cmd = zypper
		}
	}
	return cmd, args
}

// EnableService enables given service type on the system, only iscsi and multipath service types are supported
func EnableService(serviceType string) (err error) {
	log.Tracef(">>>>> EnableService called with %s", serviceType)
	defer log.Traceln("<<<<< EnableService")

	// obtain os type info
	osInfo, err := GetOsInfo()
	if err != nil {
		log.Errorln("unable to get host os information ", err.Error())
		return err
	}
	// check if systemd is supported and call generic enable function
	if osInfo.IsSystemdSupported() {
		err = enableSystemdService(osInfo, serviceType)
		return err
	}
	switch osInfo.GetOsDistro() {
	case OsTypeRedhat, OsTypeCentos, OsTypeOracle, OsTypeAmazon, OsTypeSuse:
		err = enableInitVService(osInfo, serviceType)
	case OsTypeUbuntu:
		err = enableUbuntuService(osInfo, serviceType)
	}
	return err
}

// systemdServiceUnit maps the service type to its systemd unit name.
func systemdServiceUnit(serviceType string) (string, error) {
	switch serviceType {
	case iscsi:
		return "iscsid.service", nil
	case multipath:
		return "multipathd.service", nil
	default:
		return "", errors.New("unknown service type " + serviceType + " provided")
	}
}

// systemctlExec is the exec seam for systemctl queries; tests swap it for a fake.
var systemctlExec = util.ExecCommandOutput

// IsSystemdUnitActive reports whether `systemctl is-active <unit>` answers
// "active". It is read-only, but it is still a D-Bus round trip to PID 1: while
// PID 1 is busy (e.g. in a long daemon-reload) it is not answered and the
// systemctl client gives up after its 25 s timeout, for any unit type
// (.service or .socket). Use skipSystemdCalls, which tries a check that does
// not involve systemd first.
//
// ESC-18251: on a densely loaded node a `systemctl daemon-reload` can take
// longer than the systemctl client's D-Bus timeout (25 s), the init container
// exits non-zero and the node plugin goes into Init:CrashLoopBackOff, with
// every retry asking PID 1 for yet another full reload. The services node-init
// manages (iscsid, multipathd) are almost always already active on a node that
// has been running workloads, so this assertion is used before any
// daemon-reload/enable/start/restart to skip the systemd interaction entirely
// when nothing needs to be done.
func IsSystemdUnitActive(unit string) bool {
	return systemdUnitState(unit) == unitActive
}

// unitState is what a `systemctl is-active` query established.
type unitState int

const (
	unitActive unitState = iota
	unitNotActive
	// unitNoAnswer means PID 1 did not answer the query; the state is not known.
	unitNoAnswer
)

// systemctlNoAnswerAfter is the wait from which a query that did not answer
// "active" is taken as "PID 1 did not answer". The systemctl client gives up
// after 25 s and exits 3, the same exit code as for an inactive unit, so only
// the wait tells the two apart. It is a var so unit tests can shorten it.
var systemctlNoAnswerAfter = 24 * time.Second

// systemdUnitState asks `systemctl is-active <unit>` and separates "not active"
// from "no answer". ESC-18251: a query that timed out must not be read as
// "inactive", or a busy PID 1 is asked for a reload/enable/start it cannot
// serve either.
func systemdUnitState(unit string) unitState {
	started := time.Now()
	out, _, err := systemctlExec(systemCtl, []string{"is-active", unit})
	waited := time.Since(started)
	answer := strings.TrimSpace(out)
	log.Tracef("systemctl is-active %s -> %q after %s (err=%v)", unit, answer, waited.Round(time.Millisecond), err)
	if err == nil && answer == "active" {
		return unitActive
	}
	if waited >= systemctlNoAnswerAfter {
		return unitNoAnswer
	}
	return unitNotActive
}

// isSystemctlTimeout reports whether a systemctl call failed because PID 1 did
// not answer in time: the client's own D-Bus timeout, or the command executor
// killing the client.
func isSystemctlTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timed out") || strings.Contains(msg, "killed as timeout")
}

// procNetUnixPath lists the unix sockets of the current network namespace. It
// is a var so unit tests can point it at a fixture.
var procNetUnixPath = "/proc/net/unix"

// soAcceptCon is the kernel's __SO_ACCEPTCON socket flag: set in the Flags
// column of /proc/net/unix for a socket that is listening.
const soAcceptCon = 0x10000

// serviceListenSockets maps a systemd unit to the unix socket(s) its daemon
// listens on, as they appear in /proc/net/unix ('@' prefix = abstract socket).
var serviceListenSockets = map[string][]string{
	"iscsid.service":     {"@ISCSIADM_ABSTRACT_NAMESPACE"},
	"multipathd.service": {"@/org/kernel/linux/storage/multipathd"},
}

// IsServiceSocketListening reports whether the management socket of the given
// unit's daemon is in the listening state, by reading /proc/net/unix. This is
// a plain procfs read: no D-Bus, no systemd, so it cannot be delayed by a busy
// PID 1 (ESC-18251).
//
// Abstract unix sockets are scoped to the network namespace, so this sees the
// host's daemons only because the node plugin runs with hostNetwork: true; it
// needs neither hostPID nor an extra mount. A listening socket means the
// daemon's interface is available to iscsiadm/multipathd clients: either the
// daemon itself holds it, or systemd holds it for socket activation and starts
// the daemon on the first connection.
//
// false means "not found", not "not running": callers must fall back to
// another check.
func IsServiceSocketListening(unit string) bool {
	names, ok := serviceListenSockets[unit]
	if !ok {
		return false
	}
	content, err := os.ReadFile(procNetUnixPath)
	if err != nil {
		log.Tracef("unable to read %s: %s", procNetUnixPath, err.Error())
		return false
	}
	// Num       RefCount Protocol Flags    Type St Inode Path
	// 00000000: 00000002 00000000 00010000 0001 01 22495 @ISCSIADM_ABSTRACT_NAMESPACE
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&soAcceptCon == 0 {
			continue
		}
		for _, name := range names {
			if fields[7] == name {
				log.Tracef("%s: listening socket %s found in %s", unit, name, procNetUnixPath)
				return true
			}
		}
	}
	return false
}

// skipSystemdCalls is the gate used before daemon-reload/enable/start. It
// first looks for the daemon's listening socket, which does not involve
// systemd, and only asks `systemctl is-active` when that is inconclusive.
// The calls are skipped when the service is running, and also when PID 1 did
// not answer: the state is then unknown and the unit is left untouched.
func skipSystemdCalls(unit string) bool {
	if IsServiceSocketListening(unit) {
		log.Infof("%s: the daemon's socket is listening, no systemd call is needed", unit)
		return true
	}
	switch systemdUnitState(unit) {
	case unitActive:
		log.Infof("%s is already active", unit)
		return true
	case unitNoAnswer:
		log.Warnf("systemd did not answer `systemctl is-active %s`; its state is unknown, leaving the unit untouched", unit)
		return true
	}
	return false
}

func enableSystemdService(osInfo *OsInfo, serviceType string) (err error) {
	serviceName, err := systemdServiceUnit(serviceType)
	if err != nil {
		return err
	}

	// ESC-18251: a running service needs no reload or enable, and a PID 1
	// that does not answer cannot serve one; do not ask it for anything.
	if skipSystemdCalls(serviceName) {
		log.Infof("skipping systemctl daemon-reload and enable for %s", serviceName)
		return nil
	}

	for _, args := range [][]string{{"daemon-reload"}, {"enable", serviceName}} {
		_, _, err = systemctlExec(systemCtl, args)
		if isSystemctlTimeout(err) {
			// ESC-18251: PID 1 is busy. This is not a reason to keep the
			// node plugin from starting; stop asking and carry on.
			log.Warnf("systemctl %s timed out while enabling %s, continuing without it: %s", args[0], serviceName, err.Error())
			return nil
		}
		if err != nil {
			log.Errorf("unable to enable %s service %v\n", serviceName, err.Error())
			return err
		}
	}
	return nil
}

func enableInitVService(osInfo *OsInfo, serviceType string) (err error) {
	var serviceName string
	if serviceType == iscsi {
		if osInfo.GetOsDistro() == OsTypeSuse && osInfo.GetOsMajorVersion() == "11" {
			// SUSE 11 has open-iscsi and SUSE 12 has iscsid. Only this OS has exception
			serviceName = openIscsi
		} else {
			serviceName = "iscsid"
		}
	} else if serviceType == multipath {
		serviceName = "multipathd"
	} else {
		return errors.New("unknown service type provided to enable")
	}

	args := []string{"--add", serviceName}
	_, _, err = util.ExecCommandOutput("chkconfig", args)
	if err != nil {
		log.Errorf("unable to enable %s service %v\n", serviceName, err.Error())
		return err
	}

	args = []string{serviceName, "on"}
	_, _, err = util.ExecCommandOutput("chkconfig", args)
	if err != nil {
		log.Errorf("unable to enable %s service %v\n", serviceName, err.Error())
		return err
	}
	return nil
}

func enableUbuntuService(osInfo *OsInfo, serviceType string) (err error) {
	var serviceName string
	if serviceType == iscsi {
		// generic for all distros
		serviceName = iscsid
	} else if serviceType == multipath {
		// generic for all distros
		serviceName = multipathd
	} else {
		return errors.New("unknown service type provided to enable")
	}

	args := []string{serviceName, "defaults"}
	_, _, err = util.ExecCommandOutput("update-rc.d", args)
	if err != nil {
		log.Errorf("unable to enable %s service %v\n", serviceName, err.Error())
		return err
	}
	return nil
}

// ServiceCommand runs service command on given service, valid service types are iscsi and multipath
// nolint : gocyclo
func ServiceCommand(serviceType string, operationType string) (err error) {
	log.Tracef(">>>>> ServiceCommand called with type %s op %s", serviceType, operationType)
	defer log.Traceln("<<<<< ServiceCommand")

	// validate parameters
	switch operationType {
	case "start", "stop", "status", "restart", "reload":
	default:
		return errors.New("invalid operation type " + operationType + " provided for service command")
	}

	switch serviceType {
	case iscsi, multipath:
	default:
		return errors.New("invalid service type " + serviceType + " provided for service command")
	}

	// obtain os type info
	osInfo, err := GetOsInfo()
	if err != nil {
		log.Errorln("unable to get host os information ", err.Error())
	}

	// get suitable command args based on os type, package type and install operation
	cmd, args := getServiceCommandArgs(osInfo, serviceType, operationType)

	// ESC-18251: `systemctl start` on an already-active unit is a no-op for
	// systemd but still a D-Bus round trip to a possibly busy PID 1. Check
	// first (see skipSystemdCalls) and skip the start when the service is
	// running or PID 1 does not answer.
	// Only "start" is skipped: stop/restart/reload are explicit requests to
	// change the state of a running service.
	systemdStart := cmd == systemCtl && operationType == "start"
	if systemdStart {
		if unit, uerr := systemdServiceUnit(serviceType); uerr == nil && skipSystemdCalls(unit) {
			log.Infof("skipping systemctl start %s", unit)
			return nil
		}
	}

	// The SUSE workaround ends in a daemon-reload, so it comes after the
	// check above: a multipathd that is already running does not need it.
	if osInfo.GetOsDistro() == OsTypeSuse && serviceType == multipath {
		// apply workaround for SuSE for multipathd.service, NLT-1226
		err = applySuseWorkaround()
		if err != nil {
			return err
		}
	}

	log.Infof("running %s %v", cmd, args)
	started := time.Now()
	_, rc, err := util.ExecCommandOutput(cmd, args)
	log.Infof("%s %v returned after %s", cmd, args, time.Since(started).Round(time.Millisecond))
	// ignore rc == 6 no records found error on some OS distributions as iscsiadm will attempt to to login on fresh node
	if err != nil && rc != 6 {
		if systemdStart && isSystemctlTimeout(err) {
			// ESC-18251: PID 1 is busy. A start it has read is still carried
			// out; this is not a reason to keep the node plugin from starting.
			log.Warnf("systemctl start of the %s service timed out, continuing without it: %s", serviceType, err.Error())
			return nil
		}
		log.Errorf("unable to %s %s service %v\n", operationType, serviceType, err.Error())
		return err
	}
	return nil
}

func getServiceCommandArgs(osInfo *OsInfo, packageType string, operation string) (cmd string, args []string) {
	if packageType == multipath {
		// get distro specific iscsi service names
		if osInfo.GetOsDistro() == OsTypeUbuntu && osInfo.GetOsMajorVersion() == "14" {
			// ubuntu 14.* has multipath-tools service and 16.* has multipathd.service
			args = append(args, multipathTools)
		} else {
			// generic for all distros
			args = append(args, multipathd)
		}
	} else if packageType == iscsi {
		// get distro specific iscsi service names
		if osInfo.GetOsDistro() == OsTypeSuse && osInfo.GetOsMajorVersion() == "11" {
			// suse 11.* has open-iscsi and 12.* has iscsid
			args = append(args, openIscsi)
		} else {
			args = append(args, iscsid)
		}
	}

	if osInfo.IsSystemdSupported() {
		cmd = systemCtl
		// prepend with operation for systemd commands
		args = append([]string{operation}, args...)
	} else {
		cmd = service
		args = append(args, operation)
	}
	return cmd, args
}

// ChangeMode on the device with fsMode string
func ChangeMode(devicePath, fsMode string) (err error) {
	log.Tracef(">>>>> ChangeMode to (%s) on (%s)", fsMode, devicePath)
	defer log.Trace("<<<<< ChangeMode")

	// Convert to uint value
	mode, err := strconv.ParseUint(fsMode, 8, 32)
	if err != nil {
		return fmt.Errorf("unable to parse the filesystem mode %s (%s)", fsMode, err.Error())
	}
	fileMode := os.FileMode(mode)
	log.Trace("Changing mode to ", fileMode)
	return os.Chmod(devicePath, fileMode)
}

// ChangeOwner on the mountpoint to user:group
func ChangeOwner(mountPoint, user, group string) (err error) {
	log.Tracef(">>>>> ChangeOwner to user:group (%s:%s) and mountPoint (%s)", user, group, mountPoint)
	defer log.Trace("<<<<< ChangeOwner")

	if mountPoint == "" {
		return fmt.Errorf("no mountpoint present to change ownership")
	}
	args := []string{}
	if group != "" {
		args = append(args, user+":"+group)
	} else {
		args = append(args, user)
	}
	args = append(args, mountPoint)
	_, _, err = util.ExecCommandOutput("chown", args)
	if err != nil {
		return err
	}
	return nil
}
