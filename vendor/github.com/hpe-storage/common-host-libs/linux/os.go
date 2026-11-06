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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

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
	const unitFile = "/usr/lib/systemd/system/multipathd.service"
	if _, err = os.Stat(unitFile); err == nil {
		// ESC-18251: only edit the unit and ask PID 1 for a daemon-reload when the
		// workaround has not been applied yet. On an already-patched host this
		// function used to issue a full reload on every node-plugin start.
		content, readErr := os.ReadFile(unitFile)
		if readErr != nil {
			log.Errorf("unable to read %s: %v", unitFile, readErr.Error())
			return readErr
		}
		if !suseWorkaroundNeeded(string(content)) {
			log.Tracef("suse multipathd.service workaround already applied, skipping daemon-reload")
			return nil
		}

		args := []string{"-i", "s/^ConditionKernelCommandLine=!multipath=off/#ConditionKernelCommandLine=!multipath=off/", unitFile}
		_, _, err = util.ExecCommandOutput("sed", args)
		if err != nil {
			log.Errorf("unable to apply suse workaround for multipathd.service package %v", err.Error())
			return err
		}
		args = []string{"-i", "s/^ConditionKernelCommandLine=!nompath/#ConditionKernelCommandLine=!nompath/", unitFile}
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
		// reload service with changes (the unit file was actually modified)
		args = []string{"daemon-reload"}
		_, _, err = util.ExecCommandOutput("systemctl", args)
		if err != nil {
			log.Errorf("unable to reload systemd after multipathd.service changes for sles %v\n", err.Error())
			return err
		}
	}
	return nil
}

// suseWorkaroundNeeded reports whether multipathd.service still carries an
// active (uncommented) ConditionKernelCommandLine directive that the SUSE
// workaround (NLT-1226) comments out.
func suseWorkaroundNeeded(unitContent string) bool {
	for _, line := range strings.Split(unitContent, "\n") {
		if strings.HasPrefix(line, "ConditionKernelCommandLine=!multipath=off") ||
			strings.HasPrefix(line, "ConditionKernelCommandLine=!nompath") {
			return true
		}
	}
	return false
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

// SystemctlErrorKind classifies a failed systemctl invocation so that callers can
// report what actually happened instead of guessing. ESC-18251 fix verification
// (finding F4/N3): the first fix blamed "PID 1 busy" for a 5 ms connection
// refusal, which sent support in the wrong direction.
type SystemctlErrorKind string

const (
	// SystemctlErrConnect: the client could not open the host's systemd bus at
	// all. With systemd >= 252 this is what happens from a pod's own PID
	// namespace ("Failed to connect to bus: No data available", -ENODATA from
	// the peer-credential check). It is fixed by hostPID, not by waiting.
	SystemctlErrConnect SystemctlErrorKind = "cannot connect to the host's systemd (pod PID namespace; the node DaemonSet needs hostPID=true)"
	// SystemctlErrTimeout: connected, but PID 1 did not answer within the
	// client's limit (a long daemon-reload on a densely loaded node).
	SystemctlErrTimeout SystemctlErrorKind = "timed out waiting for the host's systemd (PID 1 busy)"
	// SystemctlErrChroot: systemctl decided it runs in a chroot (/proc/1/root
	// differs from /, which is the case with hostPID=true), printed "Running in
	// chroot, ignoring command" and exited 0 WITHOUT doing anything. Needs
	// SYSTEMD_IGNORE_CHROOT=1 in the container environment.
	SystemctlErrChroot SystemctlErrorKind = "systemctl ignored the command (chroot heuristic; set SYSTEMD_IGNORE_CHROOT=1)"
	// SystemctlErrUnit: systemd answered and refused or failed the operation.
	SystemctlErrUnit SystemctlErrorKind = "systemd rejected the operation"

	// systemctlChrootMarker is the exact text systemd's verbs.c prints before
	// returning success without acting (src/shared/verbs.c, VERB_ONLINE_ONLY).
	systemctlChrootMarker = "Running in chroot, ignoring"
)

// ClassifySystemctlError maps the text of an error returned by a systemctl call
// (as wrapped by util.ExecCommandOutput) onto a SystemctlErrorKind. A nil error
// yields the empty kind.
func ClassifySystemctlError(err error) SystemctlErrorKind {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Failed to connect to bus"),
		strings.Contains(msg, "No data available"):
		return SystemctlErrConnect
	case strings.Contains(msg, "killed as timeout"),
		strings.Contains(msg, "timed out"),
		strings.Contains(msg, "Connection timed out"):
		return SystemctlErrTimeout
	case strings.Contains(msg, systemctlChrootMarker):
		return SystemctlErrChroot
	default:
		return SystemctlErrUnit
	}
}

// systemctlExec is the exec seam used by runSystemctl; tests swap it for a fake.
var systemctlExec = util.ExecCommandOutput

// runSystemctl wraps every systemctl call made on behalf of node-init.
//
// It exists because of ESC-18251 fix verification probe D3: with hostPID=true
// and no SYSTEMD_IGNORE_CHROOT, systemd 252's systemctl prints "Running in
// chroot, ignoring command '...'" at info level and exits 0 without talking to
// PID 1. The plain exec wrapper therefore reported success while the host was
// never touched, and iscsid/multipathd silently stayed unmanaged. Here that
// output is turned into an error so callers log it (or abort, in fatal mode).
// The returned error text is suitable for ClassifySystemctlError.
func runSystemctl(args ...string) (string, int, error) {
	out, rc, err := systemctlExec(systemCtl, args)
	op := "systemctl " + strings.Join(args, " ")
	if err != nil {
		return out, rc, fmt.Errorf("%s: %w", op, err)
	}
	if strings.Contains(out, systemctlChrootMarker) {
		return out, rc, fmt.Errorf("%s: %s", op, strings.TrimSpace(out))
	}
	return out, rc, nil
}

// systemdUnitDirs are the directories searched for [Install] symlinks when
// checking whether a unit is enabled: administrator (/etc), runtime (/run) and
// vendor/preset (/usr/lib) unit directories. The /host prefix covers the CSI
// node containers, which bind-mount the host root at /host.
var systemdUnitDirs = []string{
	"/etc/systemd/system", "/run/systemd/system", "/usr/lib/systemd/system",
	"/host/etc/systemd/system", "/host/run/systemd/system", "/host/usr/lib/systemd/system",
}

// systemdEnabledUnits lists, per service type, the units whose enablement is
// sufficient for the service to come up at boot. iscsid is socket-activated on
// SLES and RHEL, so an enabled iscsid.socket is as good as an enabled service.
var systemdEnabledUnits = map[string][]string{
	iscsi:     {"iscsid.service", "iscsid.socket"},
	multipath: {"multipathd.service", "multipathd.socket"},
}

func enableSystemdService(osInfo *OsInfo, serviceType string) (err error) {
	var serviceName string
	if serviceType == iscsi {
		serviceName = "iscsid.service"
	} else if serviceType == multipath {
		serviceName = "multipathd.service"
	} else {
		return errors.New("unknown service type " + serviceType + " provided to enable")
	}

	// ESC-18251: do not ask PID 1 for anything when the service is already set up
	// to start at boot. Every `systemctl daemon-reload` is a full reload of the
	// host's unit graph, which on a densely loaded node can exceed the systemctl
	// client's D-Bus timeout and previously left the CSI node-init container in
	// CrashLoopBackOff. The check is done from the filesystem (the [Install]
	// symlinks), not over D-Bus, so a busy PID 1 cannot stall it.
	for _, unit := range systemdEnabledUnits[serviceType] {
		if isSystemdUnitEnabled(unit, systemdUnitDirs) {
			log.Infof("%s is already enabled, skipping systemctl enable %s", unit, serviceName)
			return nil
		}
	}

	// No explicit daemon-reload: `systemctl enable` reloads the manager itself when
	// it changes unit files, and --no-reload avoids a second full reload. The unit
	// is picked up by the next regular reload or at boot, which is all that
	// "enable" needs to guarantee.
	args := []string{"enable", "--no-reload", serviceName}
	_, _, err = runSystemctl(args...)
	if err != nil {
		log.Errorf("unable to enable %s service (%s): %v\n", serviceName, ClassifySystemctlError(err), err.Error())
		return err
	}
	return nil
}

// isSystemdUnitEnabled reports whether the given unit has an [Install] symlink
// (<target>.wants/<unit> or <target>.requires/<unit>) under any of the given unit
// directories, i.e. `systemctl is-enabled` would report "enabled". It deliberately
// avoids D-Bus so that a busy PID 1 cannot stall or time out the check.
func isSystemdUnitEnabled(unit string, unitDirs []string) bool {
	for _, dir := range unitDirs {
		for _, suffix := range []string{"*.wants", "*.requires"} {
			matches, err := filepath.Glob(filepath.Join(dir, suffix, unit))
			if err == nil && len(matches) > 0 {
				log.Tracef("%s is enabled via %s", unit, matches[0])
				return true
			}
		}
	}
	return false
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

	if osInfo.GetOsDistro() == OsTypeSuse && serviceType == multipath {
		// apply workaround for SuSE for multipathd.service, NLT-1226
		err = applySuseWorkaround()
		if err != nil {
			return err
		}
	}

	// get suitable command args based on os type, package type and install operation
	cmd, args := getServiceCommandArgs(osInfo, serviceType, operationType)
	log.Traceln("running command ", cmd, " args: ", args)
	var rc int
	if cmd == systemCtl {
		// ESC-18251: detect the chroot no-op and keep the error text classifiable.
		_, rc, err = runSystemctl(args...)
	} else {
		_, rc, err = util.ExecCommandOutput(cmd, args)
	}
	// ignore rc == 6 no records found error on some OS distributions as iscsiadm will attempt to to login on fresh node
	if err != nil && rc != 6 {
		if cmd == systemCtl {
			log.Errorf("unable to %s %s service (%s): %v\n", operationType, serviceType, ClassifySystemctlError(err), err.Error())
		} else {
			log.Errorf("unable to %s %s service %v\n", operationType, serviceType, err.Error())
		}
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
