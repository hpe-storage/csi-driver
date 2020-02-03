// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"errors"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
	"regexp"
	"strings"
)

const (
	manufacurerPattern     = "Manufacturer:\\s*(?P<manufacturer>.*)"
	productNamePattern     = "Product Name:\\s*(?P<product>.*)"
	hypervisorTypePattern  = "vmware.*|qemu|hyper.*"
	dmiDecode              = "dmidecode"
	dmesgHypervisorPattern = "(Hypervisor.*)"
	chasisPattern          = "Chassis:\\s*(?P<chassis>.*)"
	hostnameCtl            = "hostnamectl"
	dmiSysfsPath           = "/sys/firmware/dmi/tables/DMI"
)

func getSystemInfoByParamName(parameter string) (systemInfo string, err error) {
	var out string
	var pattern string
	var result map[string]string
	args := []string{"--type", "system"}
	out, _, err = util.ExecCommandOutput(dmiDecode, args)
	if err != nil {
		log.Error("unable to get system information using dmidecode ", err.Error())
		return "", err
	}
	switch parameter {
	case "manufacturer":
		pattern = manufacurerPattern
	case "product":
		pattern = productNamePattern
	default:
		return "", errors.New("invalid parameter passed to obtain from system information")
	}
	r := regexp.MustCompile(pattern)
	// extract parameter
	if r.MatchString(out) {
		result = util.FindStringSubmatchMap(out, r)
		return result[parameter], nil
	}
	return "", errors.New("parameter not found in system information string: " + parameter)
}

// get system chassis info using hostnamectl, eg output below:
// Static hostname: host-name.domain.com
// Icon name: computer-vm
// Chassis: vm
// Machine ID: 7b5fe956062b4bd6b9fbce3b2bae52a0
// Boot ID: e2aa443f9b3e453e82ade336f960efea
// Virtualization: vmware
// Operating System: OpenStack
// CPE OS Name: cpe:/o:redhat:enterprise_linux:7.4:GA:server
// Kernel: Linux 3.10.0-693.el7.x86_64
// Architecture: x86-64
func getSystemChassisInfo() (chassisInfo string, err error) {
	out, _, err := util.ExecCommandOutput(hostnameCtl, nil)
	if err != nil {
		// log as debug, as we fall back to other alternatives to figure
		// this out if hostnamectl is not available
		log.Debug("unable to get system chassis info using hostnamectl", err.Error())
		return "", err
	}
	r := regexp.MustCompile(chasisPattern)
	if r.MatchString(out) {
		result := util.FindStringSubmatchMap(out, r)
		return result["chassis"], nil
	}
	return "", errors.New("chassis info not found in system information using hostnamectl")
}

// GetManufacturer return manufacturer name of the system
func GetManufacturer() (manufacturer string, err error) {
	manufacturer, err = getSystemInfoByParamName("manufacturer")
	if err != nil {
		return "", err
	}
	log.Trace("got system manufacturer: ", manufacturer)
	return strings.TrimSpace(strings.ToLower(manufacturer)), nil
}

// GetProductName return product name of the system
func GetProductName() (productName string, err error) {
	productName, err = getSystemInfoByParamName("product")
	if err != nil {
		return "", err
	}
	log.Trace("got system product name: ", productName)
	return strings.TrimSpace(strings.ToLower(productName)), nil
}

// IsVirtualMachine returns true if system is running as a guest on hypervisor
func IsVirtualMachine() (isVM bool, err error) {
	var manufacturerName string
	isVM = false
	// fall back to get details using dmidecode
	manufacturerName, err = GetManufacturer()
	if err != nil {
		// dmidecode can be missing, perform another best attempt to determine if running as vm using sysfs
		lines, err2 := util.FileGetStringsWithPattern(dmiSysfsPath, hypervisorTypePattern)
		if err2 != nil {
			log.Error("unable to get system information using sysfs as well ", err2.Error())
			// return original error with dmidecode
			return false, errors.New("cannot determine if system is of type virtual machine, " + err.Error())
		}
		if len(lines) > 0 {
			isVM = true
		}
		log.Trace("DMI-SYSFS: System is running as a virtual machine")
		return isVM, nil
	}
	// check if product name matches any of the hypervisor types(vmware, kvm or hyperV)
	r := regexp.MustCompile(hypervisorTypePattern)
	if r.MatchString(manufacturerName) {
		isVM = true
		log.Trace("DMI: System is running as a virtual machine")
	}
	return isVM, nil
}
