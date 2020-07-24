// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
	"io/ioutil"
	"strings"
)

const fcHostBasePath = "/sys/class/fc_host"
const fcHostPortNameFormat = "/sys/class/fc_host/host%s/port_name"
const fcHostNodeNameFormat = "/sys/class/fc_host/host%s/node_name"
const fcHostScanPathFormat = "/sys/class/scsi_host/host%s/scan"

// FcHostLIPNameFormat :
const FcHostLIPNameFormat = "/sys/class/fc_host/host%s/issue_lip"

// GetHostPort get the host port details for given host number from H:C:T:L of device
func GetHostPort(hostNumber string) (hostPort *model.FcHostPort, err error) {
	hostPath := fmt.Sprintf(fcHostPortNameFormat, hostNumber)
	portName, err := util.FileReadFirstLine(hostPath)
	if err != nil {
		log.Warnf("unable to get port WWN for host %s, error %s", hostNumber, err.Error())
		return nil, err
	}
	log.Tracef("got port WWN %s for host %s", portName, hostNumber)
	hostPath = fmt.Sprintf(fcHostNodeNameFormat, hostNumber)
	nodeName, err := util.FileReadFirstLine(hostPath)
	if err != nil {
		log.Warnf("unable to get node WWN for host %s, error %s", hostNumber, err.Error())
		return nil, err
	}
	log.Tracef("got node WWN %s for host %s", nodeName, hostNumber)
	hostPort = &model.FcHostPort{HostNumber: hostNumber, NodeWwn: strings.TrimPrefix(nodeName, "0x"), PortWwn: strings.TrimPrefix(portName, "0x")}
	return hostPort, nil
}

// GetAllFcHostPorts get all the FC host port details on the host
func GetAllFcHostPorts() (hostPorts []*model.FcHostPort, err error) {
	log.Tracef("GetAllFcHostPorts called")
	var hostNumbers []string
	args := []string{"-1", fcHostBasePath}
	exists, _, err := util.FileExists(fcHostBasePath)
	if !exists {
		log.Warn("no fc adapters found on the host")
		return nil, nil
	}

	out, _, err := util.ExecCommandOutput("ls", args)
	if err != nil {
		log.Warnf("unable to get list of host fc ports, error %s", err.Error())
		return nil, err
	}

	hostNumbers = strings.Split(out, "\n")
	if len(hostNumbers) == 0 {
		log.Errorf("no fc adapters found on the host")
		return nil, nil
	}

	for _, host := range hostNumbers {
		if host != "" {
			hostPort, err := GetHostPort(strings.TrimPrefix(host, "host"))
			if err != nil {
				log.Warnf("unable to get details of fc host port %s, error %s", host, err.Error())
				continue
			}
			hostPorts = append(hostPorts, hostPort)
		}
	}
	return hostPorts, nil
}

// GetAllFcHostPortWWN get all FC host port WWN's on the host
func GetAllFcHostPortWWN() (portWWNs []string, err error) {
	log.Tracef("GetAllFcHostPortWWN called")
	hostPorts, err := GetAllFcHostPorts()
	if err != nil {
		return nil, err
	}
	if len(hostPorts) == 0 {
		return nil, nil
	}
	var inits []string
	for _, hostPort := range hostPorts {
		inits = append(inits, hostPort.PortWwn)
	}
	return inits, nil
}

// RescanFcTarget rescans host ports for new Fibre Channel devices
//nolint: dupl
func RescanFcTarget(lunID string) (err error) {
	log.Tracef(">>> RescanFcTarget called on lunID %s", lunID)
	defer log.Traceln("<<< RescanFcTarget")

	// Get the list of FC hosts to rescan
	fcHosts, err := GetAllFcHostPorts()
	if err != nil {
		return err
	}
	for _, fcHost := range fcHosts {
		// perform rescan for all devices
		fcHostScanPath := fmt.Sprintf(fcHostScanPathFormat, fcHost.HostNumber)
		isFCHostScanPathExists, _, _ := util.FileExists(fcHostScanPath)
		if !isFCHostScanPathExists {
			log.Tracef("fc host scan path %s does not exist", fcHostScanPath)
			continue
		}
		var err error
		if lunID == "" {
			// fallback to the generic host rescan
			err = ioutil.WriteFile(fcHostScanPath, []byte("- - -"), 0644)
			if err != nil {
				log.Debugf("error writing to file %s : %s", fcHostScanPath, err.Error())
			}
		} else {
			log.Tracef("\n SCANNING fc lun id %s", lunID)
			err = ioutil.WriteFile(fcHostScanPath, []byte("- - "+lunID), 0644)
			if err != nil {
				log.Debugf("error writing to file %s : %s", fcHostScanPath, err.Error())
			}
		}
		if err != nil {
			log.Errorf("unable to rescan for fc devices on host port :%s lun: %s err %s", fcHost.HostNumber, lunID, err.Error())
			return err
		}
	}
	return nil
}

// verifies if the scsi slaves are fc devices are not
func isFibreChannelDevice(slaves []string) bool {
	log.Tracef("isFibreChannelDevice called")
	// time.Sleep(time.Duration(1) * time.Second)
	for _, slave := range slaves {
		log.Tracef("handling path %s", slave)
		args := []string{"-l", "/sys/block/" + slave}
		out, _, _ := util.ExecCommandOutput("ls", args)
		if strings.Contains(out, "rport") {
			log.Tracef("%s is a FC device", slave)
			return true
		}
	}
	return false
}
