// Copyright 2025 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	"io/ioutil"
	"os"
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
    nvmecmd                = "nvme"
    nvmeConnectCmd         = "nvme connect"
    nvmeDisconnectCmd      = "nvme disconnect"
    nvmeListCmd            = "nvme list"
    nvmeListSubsysCmd      = "nvme list-subsys"
    defaultNvmePort        = "4420"
    nvmeHostPathFormat     = "/sys/class/nvme/"
    nvmeNamespacePattern   = "nvme[0-9]+n[0-9]+"
    nvmeHostPath           = "/etc/nvme/hostnqn"
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

    // Example: Set NVMe core parameters (if needed)
    if err := setKernelParam("/sys/module/nvme_core/parameters/multipath", "Y"); err != nil {
        tuningErrors = append(tuningErrors, err.Error())
    }

    // Add more NVMe/TCP-specific tuning as needed...

    if len(tuningErrors) > 0 {
        return fmt.Errorf("NVMe TCP tuning errors: %s", strings.Join(tuningErrors, "; "))
    }
    return nil
}

func setSysctl(key, value string) error {
    cmd := fmt.Sprintf("sysctl -w %s=%s", key, value)
    out, _, err := util.ExecCommandOutput("sh", []string{"-c", cmd})
    if err != nil {
        return fmt.Errorf("failed to set %s: %v (%s)", key, err, out)
    }
    return nil
}

func setKernelParam(path, value string) error {
    f, err := os.OpenFile(path, os.O_WRONLY, 0)
    if err != nil {
        return fmt.Errorf("failed to open %s: %v", path, err)
    }
    defer f.Close()
    if _, err := f.WriteString(value); err != nil {
        return fmt.Errorf("failed to write %s to %s: %v", value, path, err)
    }
    return nil
}

// ConnectNvmeTarget connects to an NVMe over TCP target
func ConnectNvmeTarget(target *model.NvmeTarget) error {
    discoveryIPs := strings.Split(target.Address, ",")
    if len(discoveryIPs) == 0 {
        return fmt.Errorf("no discovery IPs provided for NVMe target")
    }

    var lastErr error
    var success bool

    for _, ip := range discoveryIPs {
        args := []string{
            "connect",
            "-t", "tcp",
            "-n", target.NQN,
            "-a", ip,
            "-s", target.Port,
        }
        _, rc, err := util.ExecCommandOutput(nvmecmd, args)
        if err != nil && rc != 114 {
            log.Warnf("NVMe connect failed for discovery IP %s, rc=%d, error: %s", ip, rc, err)
            lastErr = err
            continue
        }
        log.Infof("Successfully connected to NVMe target using discovery IP %s", ip)
        success = true
    }

    if !success {
        return fmt.Errorf("failed to connect to NVMe target: %v", lastErr)
    }
    return nil
}

// RescanNvme performs NVMe namespace rescan
func RescanNvme() error {
    // NVMe typically doesn't require explicit rescanning like SCSI
    // The kernel automatically detects new namespaces
    return nil
}
// HandleNvmeTcpDiscovery performs NVMe/TCP connection and device verification for a volume.
func HandleNvmeTcpDiscovery(volume *model.Volume) error {
    log.Tracef(">>>>> HandleNvmeTcpDiscovery for volume %s", volume.SerialNumber)
    defer log.Trace("<<<<< HandleNvmeTcpDiscovery")

    // 1. Apply NVMe/TCP tuning recommendations
    if err := ApplyNvmeTcpTuning(); err != nil {
        log.Warnf("Failed to apply NVMe TCP tuning: %v", err)
        // Continue even if tuning fails
    }

    // 2. Prepare NVMe target info
    target := &model.NvmeTarget{
        NQN:     volume.Nqn,
        Address: strings.Join(volume.DiscoveryIPs, ","),
        Port:    volume.TargetPort,
    }

    // 3. Connect to NVMe target
    if err := ConnectNvmeTarget(target); err != nil {
        return fmt.Errorf("failed to connect to NVMe target: %v", err)
    }

    // 4. Optionally, verify device presence (wait for /dev/nvmeXnY)
    found := false
    for i := 0; i < 10; i++ {
        devices, _ := FindNvmeDevices(volume.SerialNumber)
        if len(devices) > 0 {
            found = true
            break
        }
        time.Sleep(1 * time.Second)
    }
    if !found {
        return fmt.Errorf("NVMe device for serial %s not found after connect", volume.SerialNumber)
    }

    return nil
}

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