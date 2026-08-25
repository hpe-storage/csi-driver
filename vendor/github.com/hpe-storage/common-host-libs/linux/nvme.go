// Copyright 2025 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
)

const (
	nvmecmd              = "nvme"
	nvmeConnectCmd       = "nvme connect"
	nvmeDisconnectCmd    = "nvme disconnect"
	nvmeListCmd          = "nvme list"
	nvmeListSubsysCmd    = "nvme list-subsys"
	// defaultNvmePort      = "4420"
	// nvmeDiscoveryPort    = "8009"
	nvmeHostPathFormat   = "/sys/class/nvme/"
	nvmeNamespacePattern = "nvme[0-9]+n[0-9]+"
	nvmeHostPath         = "/etc/nvme/hostnqn"

	// nvmeDiscoveryNQN is the well-known NVMe discovery subsystem NQN. Discovery
	// controllers created by "nvme discover" register under this NQN.
	nvmeDiscoveryNQN = "nqn.2014-08.org.nvmexpress.discovery"
)

// GetNvmeInitiator gets the NVMe host NQN
func GetNvmeInitiator() (string, error) {
	// Read from /etc/nvme/hostnqn or generate one if not present
	hostnqn, err := util.FileReadFirstLine(nvmeHostPath)
	if err != nil {
		log.Debugf("Could not read hostnqn from %s, generating one", nvmeHostPath)
		// Generate hostnqn using nvme gen-hostnqn
		args := []string{"gen-hostnqn"}
		hostnqn, _, err = util.ExecCommandOutput(nvmecmd, args)
		if err != nil {
			return "", err
		}
		hostnqn = strings.TrimSpace(hostnqn)
	}
	return hostnqn, nil
}

// ApplyNvmeTcpTuning applies recommended sysctl and module settings for NVMe over TCP
func ApplyNvmeTcpTuning() error {
	var tuningErrors []string

	// Example: Increase network buffer sizes for high throughput
	if err := setSysctl("net.core.rmem_max", netCoreRmemMax); err != nil {
		tuningErrors = append(tuningErrors, err.Error())
	}
	if err := setSysctl("net.core.wmem_max", netCoreWmemMax); err != nil {
		tuningErrors = append(tuningErrors, err.Error())
	}

	// Check native NVMe multipath. nvme_core.multipath is a read-only (0444)
	// kernel module parameter: it can only be set at boot/module-load time and
	// cannot be changed at runtime by writing sysfs, not even from a privileged
	// container. We therefore only verify (never write) its current value.
	logNvmeMultipathStatus()

	// Add more NVMe/TCP-specific tuning as needed...

	if len(tuningErrors) > 0 {
		return fmt.Errorf("NVMe TCP tuning errors: %s", strings.Join(tuningErrors, "; "))
	}
	return nil
}

// nvmeCoreMultipathPath is the sysfs path for the nvme_core native multipath
// module parameter. It is a variable so unit tests can override it.
var nvmeCoreMultipathPath = "/sys/module/nvme_core/parameters/multipath"

// nvmeExecCommandOutput runs an external command and returns its stdout, return
// code, and error. It is a variable so unit tests can override it.
var (
	nvmeExecCommandOutput       = util.ExecCommandOutput
	nvmeControllerReadDir       = ioutil.ReadDir
	nvmeControllerReadFirstLine = util.FileReadFirstLine
	nvmeFindDevices             = FindNvmeDevices
	nvmeApplyTcpTuning          = ApplyNvmeTcpTuning
)

// isNvmeMultipathEnabled reports whether native NVMe multipath is currently
// enabled on this node by reading the read-only nvme_core.multipath module
// parameter.
func isNvmeMultipathEnabled() (bool, error) {
	value, err := util.FileReadFirstLine(nvmeCoreMultipathPath)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(value), "Y"), nil
}

// logNvmeMultipathStatus checks the read-only nvme_core.multipath module
// parameter and logs whether native NVMe multipath is enabled. The parameter
// cannot be enabled at runtime (privileged containers included); it must be set
// at boot/module-load time.
func logNvmeMultipathStatus() {
	enabled, err := isNvmeMultipathEnabled()
	if err != nil {
		log.Infof("Could not read NVMe native multipath parameter %s: %v. Kernel may lack NVMe multipath support.", nvmeCoreMultipathPath, err)
		return
	}
	if enabled {
		log.Tracef("NVMe native multipath is enabled (%s=Y)", nvmeCoreMultipathPath)
		return
	}
	log.Warnf("NVMe native multipath is disabled at %s. This is a read-only kernel "+
		"module parameter and cannot be enabled at runtime (privileged containers "+
		"included). To enable it, add 'nvme_core.multipath=Y' to the kernel command "+
		"line or 'options nvme_core multipath=Y' in /etc/modprobe.d, then reboot or "+
		"reload the nvme_core module.", nvmeCoreMultipathPath)
}

func setSysctl(key, value string) error {
	cmd := fmt.Sprintf("sysctl -w %s=%s", key, value)
	out, _, err := util.ExecCommandOutput("sh", []string{"-c", cmd})
	if err != nil {
		return fmt.Errorf("failed to set %s: %v (%s)", key, err, out)
	}
	return nil
}

// ConnectNvmeTarget connects to an NVMe over TCP target. liveAddrs is the set
// of portal IPs (as returned by getLiveNvmeControllersForNQN) already known to
// have a live controller for target.NQN; pass the caller's already-computed
// result (e.g. from HandleNvmeTcpDiscovery) to avoid a redundant
// /sys/class/nvme scan here, or nil/empty if not available.
func ConnectNvmeTarget(target *model.NvmeTarget, liveAddrs map[string]bool) error {
	if strings.TrimSpace(target.Address) == "" {
		return fmt.Errorf("no target address provided for NVMe target %s", target.NQN)
	}

	// Sanitize and dedupe up front: a repeated or blank entry in target.Address
	// (e.g. a trailing comma) must not turn into a wasted/confusing connect attempt.
	var targetIPs []string
	seenIPs := make(map[string]bool)
	for _, ip := range strings.Split(target.Address, ",") {
		ip = util.SanitizeIPAddress(ip)
		if ip == "" || seenIPs[ip] {
			continue
		}
		seenIPs[ip] = true
		targetIPs = append(targetIPs, ip)
	}
	if len(targetIPs) == 0 {
		return fmt.Errorf("no valid target IPs found in target address %q for NVMe target %s", target.Address, target.NQN)
	}

	var success bool
	var failedIPs []string

	for _, ip := range targetIPs {
		if liveAddrs[ip] {
			log.Infof("NVMe target %s already connected via %s:%s, skipping redundant connect", target.NQN, ip, target.Port)
			success = true
			continue
		}
		args := []string{
			"connect",
			"-t", "tcp",
			"-n", target.NQN,
			"-a", ip,
			"-s", target.Port,
		}
		out, rc, err := nvmeExecCommandOutput(nvmecmd, args)
		// Treat rc=114 as success
		if rc == 114 {
			log.Infof("NVMe target %s connected via %s:%s (rc=114)", target.NQN, ip, target.Port)
			success = true
			continue
		}
		// Handle non-zero rc with 'already connected' message
		if err != nil || rc != 0 {
			if strings.Contains(strings.ToLower(out), "already connected") {
				log.Infof("NVMe target %s already connected via %s:%s", target.NQN, ip, target.Port)
				success = true
				continue
			}
			log.Warnf("NVMe connect failed for discovery IP %s, rc=%d, error: %v", ip, rc, err)
			failedIPs = append(failedIPs, ip)
			continue
		}
		log.Infof("Successfully connected to NVMe target using discovery IP %s", ip)
		success = true
	}

	// A subsystem can be considered connected once any one portal is live (multipath
	// tolerates partial connectivity), but a partially-failed portal must stay visible
	// so callers don't mistake this for full connectivity across all supplied portals.
	if len(failedIPs) > 0 {
		log.Warnf("NVMe target %s: %d of %d portal(s) failed to connect: %v", target.NQN, len(failedIPs), len(targetIPs), failedIPs)
	}

	if !success {
		return fmt.Errorf("failed to connect to NVMe target %s: all portal(s) failed: %v", target.NQN, failedIPs)
	}
	return nil
}

// RescanNvme performs NVMe namespace rescan
func RescanNvme() error {
	// NVMe typically doesn't require explicit rescanning like SCSI
	// The kernel automatically detects new namespaces
	return nil
}

// allTargetPortalsLive reports whether every portal in volume.TargetAddress is
// already live. Used by Fix 1 so a subsystem with only *some* portals connected
// (e.g. one of two multipath portals) keeps retrying the missing ones on every
// call instead of being mistaken for fully staged after just one portal comes up.
func allTargetPortalsLive(volume *model.Volume, liveAddrs map[string]bool) bool {
	if strings.TrimSpace(volume.TargetAddress) == "" {
		// No static portal list to compare against; fall back to "any live" semantics.
		return len(liveAddrs) > 0
	}
	for _, ip := range strings.Split(volume.TargetAddress, ",") {
		if !liveAddrs[util.SanitizeIPAddress(ip)] {
			return false
		}
	}
	return true
}

// getLiveNvmeControllersForNQN scans /sys/class/nvme and returns the portal IPs
// of controllers whose subsysnqn matches nqn and whose state is "live". Used to
// avoid redundant nvme discover/connect calls against an already-connected subsystem.
func getLiveNvmeControllersForNQN(nqn string) (map[string]bool, error) {
	addrs := make(map[string]bool)
	if nqn == "" {
		return addrs, nil
	}
	entries, err := nvmeControllerReadDir(nvmeHostPathFormat)
	if err != nil {
		return addrs, err
	}
	for _, e := range entries {
		subnqn, err := nvmeControllerReadFirstLine(fmt.Sprintf("/sys/class/nvme/%s/subsysnqn", e.Name()))
		if err != nil || strings.TrimSpace(subnqn) != nqn {
			continue
		}
		state, _ := nvmeControllerReadFirstLine(fmt.Sprintf("/sys/class/nvme/%s/state", e.Name()))
		if strings.TrimSpace(state) != "live" {
			continue
		}
		addr, _ := nvmeControllerReadFirstLine(fmt.Sprintf("/sys/class/nvme/%s/address", e.Name()))
		if ip, _ := parseNvmeAddress(strings.TrimSpace(addr)); ip != "" {
			// Sanitize so IPv4/IPv6 keys match the sanitized IPs used elsewhere (ConnectNvmeTarget).
			addrs[util.SanitizeIPAddress(ip)] = true
		}
	}
	return addrs, nil
}

// HandleNvmeTcpDiscovery performs NVMe/TCP connection and device verification for a volume.
func HandleNvmeTcpDiscovery(volume *model.Volume) error {
	if volume == nil {
		return fmt.Errorf("HandleNvmeTcpDiscovery: input argument volume-info is nil")
	}
	log.Tracef(">>>>> HandleNvmeTcpDiscovery for volume %s", volume.SerialNumber)
	defer log.Trace("<<<<< HandleNvmeTcpDiscovery")

	// liveAddrs backs both Fix 1 and Fix 2: a namespace device with no live
	// controller behind it (e.g. a stale entry left over after a connection
	// loss with no auto-reconnect) must not be mistaken for a staged volume.
	liveAddrs, liveAddrsErr := getLiveNvmeControllersForNQN(volume.Nqn)
	if liveAddrsErr != nil {
		log.Warnf("Could not inspect live NVMe/TCP controllers for NQN %s: %v; continuing with discovery/connect", volume.Nqn, liveAddrsErr)
	} else {
		log.Tracef("Live NVMe/TCP controllers for NQN %s: %v", volume.Nqn, liveAddrs)
	}

	// Fix 1: nothing to do only if the device exists AND every known target
	// portal is live (not just one) - otherwise a still-down portal would never
	// be retried again once the volume's first portal comes up.
	allPortalsLive := allTargetPortalsLive(volume, liveAddrs)
	log.Tracef("NVMe/TCP stage state for serial %s, NQN %s: target portals=%q, all live=%t", volume.SerialNumber, volume.Nqn, volume.TargetAddress, allPortalsLive)
	if allPortalsLive {
		if devs, _ := nvmeFindDevices(volume.SerialNumber); len(devs) > 0 {
			log.Infof("NVMe device for serial %s already present and connected, skipping discovery/connect", volume.SerialNumber)
			return nil
		}
		log.Infof("All NVMe/TCP target portals for NQN %s are live, but device serial %s is absent; verifying namespace after connect", volume.Nqn, volume.SerialNumber)
	}

	if err := nvmeApplyTcpTuning(); err != nil {
		log.Warnf("Failed to apply NVMe TCP tuning: %v", err)
		// Continue even if tuning fails
	}

	// Default to the CSP-supplied target; only overridden if discovery runs and finds portals.
	target := &model.NvmeTarget{
		NQN:     volume.Nqn,
		Address: volume.TargetAddress,
		Port:    volume.TargetPort,
	}
	log.Tracef("Using CSP-supplied NVMe/TCP target for NQN %s: address=%q, port=%q", target.NQN, target.Address, target.Port)

	// Fix 2: the CSP already supplies the full, current I/O portal list in
	// TargetAddress/TargetPort (device.go derives it directly from
	// volume.DiscoveryIPs), so nvme discover would only re-fetch data we already
	// have while creating the orphan-prone discovery controller. Connect using
	// the CSP-supplied target directly instead.
	log.Infof("NVMe/TCP discovery skipped for NQN %s; connecting directly using CSP-supplied target (live portals: %v)", volume.Nqn, liveAddrs)

	// Fix 3 (inside ConnectNvmeTarget) skips nvme connect for portals already live.
	log.Tracef("Connecting NVMe/TCP target for NQN %s: address=%q, port=%q", target.NQN, target.Address, target.Port)
	if err := ConnectNvmeTarget(target, liveAddrs); err != nil {
		return fmt.Errorf("failed to connect to NVMe target %s: %v", volume.Nqn, err)
	}
	log.Infof("NVMe/TCP connect completed for NQN %s", volume.Nqn)

	// Verify device presence (wait for /dev/nvmeXnY)
	found := false
	for i := 0; i < 10; i++ {
		devices, _ := nvmeFindDevices(volume.SerialNumber)
		if len(devices) > 0 {
			log.Infof("Found NVMe device(s) for serial %s after %d verification attempt(s): %v", volume.SerialNumber, i+1, devices)
			found = true
			break
		}
		log.Tracef("NVMe device for serial %s not present after connect (attempt %d/10)", volume.SerialNumber, i+1)
		time.Sleep(1 * time.Second)
	}
	if !found {
		return fmt.Errorf("NVMe device for serial %s not found after connect", volume.SerialNumber)
	}

	return nil
}

// // discoverNvmeTarget runs nvme discover for volume's NQN and returns a target
// // built from the discovered I/O portals, or nil if none were found.
// func discoverNvmeTarget(volume *model.Volume) *model.NvmeTarget {
// 	discoveryIPs := volume.DiscoveryIPs
// 	if len(discoveryIPs) == 0 && strings.TrimSpace(volume.TargetAddress) != "" {
// 		discoveryIPs = strings.Split(volume.TargetAddress, ",")
// 	}
// 	endpoints, err := discoverNvmeEndpoints(volume.Nqn, discoveryIPs)
// 	if err != nil {
// 		log.Warnf("NVMe discover failed on %v: %v", discoveryIPs, err)
// 	}
// 	if len(endpoints) == 0 {
// 		return nil
// 	}

// 	var epIPs []string
// 	var epPort string
// 	seenIPs := make(map[string]bool)
// 	for _, ep := range endpoints {
// 		ip := util.SanitizeIPAddress(ep.IP)
// 		if ip != "" && !seenIPs[ip] {
// 			epIPs = append(epIPs, ip)
// 			seenIPs[ip] = true
// 		}
// 		if epPort == "" && strings.TrimSpace(ep.Port) != "" {
// 			epPort = strings.TrimSpace(ep.Port)
// 		}
// 	}
// 	if epPort == "" {
// 		epPort = defaultNvmePort
// 	}
// 	return &model.NvmeTarget{
// 		NQN:     volume.Nqn,
// 		Address: strings.Join(epIPs, ","),
// 		Port:    epPort,
// 	}
// }

// nvmeEndpoint represents an NVMe/TCP I/O portal discovered via nvme discover
// type nvmeEndpoint struct {
// 	IP     string
// 	Port   string
// 	Subnqn string
// }

// // discoverNvmeEndpoints runs nvme discover on the given discovery IPs and returns I/O portals matching the NQN
// func discoverNvmeEndpoints(nqn string, discoveryIPs []string) ([]nvmeEndpoint, error) {
// 	var eps []nvmeEndpoint
// 	if len(discoveryIPs) == 0 {
// 		return eps, fmt.Errorf("no discovery IPs provided")
// 	}
// 	// "nvme discover" leaves a persistent discovery controller behind on every
// 	// call. Reap them once discovery is done so they don't accumulate as orphaned
// 	// "live" discovery controllers - otherwise they pile up (thousands over time)
// 	// and make every subsequent NVMe operation on the node extremely slow because
// 	// the kernel has to walk every controller.
// 	defer reapNvmeDiscoveryControllers()
// 	for _, ip := range discoveryIPs {
// 		// Sanitize IP address (remove whitespace,* and other invalid format)
// 		ip = util.SanitizeIPAddress(ip)
// 		args := []string{
// 			"discover",
// 			"-t", "tcp",
// 			"-a", ip,
// 			"-s", nvmeDiscoveryPort,
// 		}
// 		out, rc, err := nvmeExecCommandOutput(nvmecmd, args)
// 		if err != nil || rc != 0 {
// 			log.Warnf("NVMe discover failed on %s:%s: %v", ip, nvmeDiscoveryPort, err)
// 			continue
// 		}
// 		parsed := parseDiscoveryOutput(out, nqn)
// 		eps = append(eps, parsed...)
// 	}
// 	return eps, nil
// }

// reapNvmeDiscoveryControllers disconnects all NVMe discovery controllers
// (subsystem NQN nqn.2014-08.org.nvmexpress.discovery). Discovery controllers
// carry no namespaces, so disconnecting them is always safe. This prevents the
// unbounded accumulation of orphaned discovery controllers created by
// "nvme discover" on every NodeStageVolume.
func reapNvmeDiscoveryControllers() {
	out, _, err := nvmeExecCommandOutput(nvmecmd, []string{"disconnect", "-n", nvmeDiscoveryNQN})
	if err != nil {
		log.Tracef("No NVMe discovery controllers to reap (or disconnect failed): %v (%s)", err, strings.TrimSpace(out))
		return
	}
	log.Debugf("Reaped NVMe discovery controllers (%s): %s", nvmeDiscoveryNQN, strings.TrimSpace(out))
}

// // parseDiscoveryOutput extracts traddr/trsvcid/subnqn entries from nvme discover output
// func parseDiscoveryOutput(output string, wantedNqn string) []nvmeEndpoint {
// 	lines := strings.Split(output, "\n")
// 	var eps []nvmeEndpoint
// 	var cur nvmeEndpoint
// 	for _, line := range lines {
// 		l := strings.TrimSpace(line)
// 		if l == "" {
// 			continue
// 		}
// 		if strings.HasPrefix(l, "=====") {
// 			// new entry delimiter; flush previous if complete
// 			if cur.IP != "" && cur.Port != "" && cur.Subnqn != "" {
// 				if wantedNqn == "" || cur.Subnqn == wantedNqn {
// 					eps = append(eps, cur)
// 				}
// 			}
// 			cur = nvmeEndpoint{}
// 			continue
// 		}
// 		if strings.HasPrefix(l, "subnqn:") {
// 			cur.Subnqn = strings.TrimSpace(strings.TrimPrefix(l, "subnqn:"))
// 			continue
// 		}
// 		if strings.HasPrefix(l, "traddr:") {
// 			// Sanitize so IPv4/IPv6 traddr values match cleanly downstream.
// 			cur.IP = util.SanitizeIPAddress(strings.TrimSpace(strings.TrimPrefix(l, "traddr:")))
// 			continue
// 		}
// 		if strings.HasPrefix(l, "trsvcid:") {
// 			cur.Port = strings.TrimSpace(strings.TrimPrefix(l, "trsvcid:"))
// 			continue
// 		}
// 	}
// 	// flush last entry
// 	if cur.IP != "" && cur.Port != "" && cur.Subnqn != "" {
// 		if wantedNqn == "" || cur.Subnqn == wantedNqn {
// 			eps = append(eps, cur)
// 		}
// 	}
// 	return eps
// }

// DisconnectNVMeTargetByNQN disconnects all NVMe controllers for a given subsystem NQN
func DisconnectNVMeTargetByNQN(subsysNQN string) error {
	if subsysNQN == "" {
		return fmt.Errorf("subsystem NQN is empty")
	}
	cmd := exec.Command("nvme", "disconnect", "-n", subsysNQN)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("Failed to disconnect NVMe subsystem NQN %s: %s, output: %s", subsysNQN, err.Error(), string(output))
		return err
	}
	log.Infof("Disconnected NVMe subsystem NQN %s successfully", subsysNQN)
	return nil
}

// FindNvmeDevices searches for NVMe devices matching the given serial number
func FindNvmeDevices(serialNumber string) ([]string, error) {
	var devices []string

	// Scan /dev for nvme devices
	files, err := ioutil.ReadDir("/dev")
	if err != nil {
		return nil, err
	}

	nvmeRegex := regexp.MustCompile(`^nvme\d+n\d+$`)
	for _, f := range files {
		if nvmeRegex.MatchString(f.Name()) {
			devicePath := filepath.Join("/dev", f.Name())

			// Check serial number via sysfs
			sysfsSerialPath := fmt.Sprintf("/sys/class/block/%s/subsystem/%s/nguid", f.Name(), f.Name())
			log.Tracef("serial path=%s", sysfsSerialPath)
			if serial, err := util.FileReadFirstLine(sysfsSerialPath); err == nil {
				// Normalize the serial from sysfs by removing dashes and whitespace
				normalizedSerial := strings.ReplaceAll(strings.TrimSpace(serial), "-", "")
				log.Tracef("found serial number %s, normalized: %s", serial, normalizedSerial)
				if strings.TrimSpace(normalizedSerial) == serialNumber {
					devices = append(devices, devicePath)
				}
			}

			// Also check if the device name itself matches (for namespace matching)
			if f.Name() == serialNumber {
				devices = append(devices, devicePath)
			}
		}
	}

	return devices, nil
}
