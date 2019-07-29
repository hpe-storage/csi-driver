// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
	"regexp"
	"strconv"
	"strings"
)

const (
	lsblkcommand = "lsblk"
	lsblkPattern = "(?P<name>[\\w,:;\\-_~]+)\\s+(?P<nickname>\\(.+\\)\\s+)*(?P<type>\\w+)\\s+(?P<size>\\w+)"
	partType     = "part"
)

func getLargestPartition(partInfos []model.DevicePartition) (partInfo *model.DevicePartition) {
	var largest *model.DevicePartition
	largest = nil
	if len(partInfos) != 0 {
		for _, partition := range partInfos {
			log.Tracef("handling parition :%+v", partition)
			if largest == nil && partition.Partitiontype == partType {
				largest = &partition
				log.Tracef("setting largest partition as %+v", partition)
			} else {
				if partition.Size > largest.Size && partition.Partitiontype == partType {
					log.Tracef("setting largest partition as %+v as it is greater than %+v", partition, largest)
					largest = &partition
				}
			}
		}
	}
	return largest
}

// GetPartitionInfo  for the Device dev
func GetPartitionInfo(dev *model.Device) (partInfo []model.DevicePartition, err error) {
	log.Tracef(">>>>> GetPartitionInfo called for device %+v", dev)
	defer log.Trace("<<<<< GetPartitionInfo")

	args := []string{"-b", "-l", "-o", "NAME,TYPE,SIZE"}

	if dev.AltFullPathName == "" {
		err = fmt.Errorf("Pathname not set for device %s", dev.AltFullPathName)
		log.Tracef(err.Error())
		return nil, err
	}

	args = append(args, dev.AltFullPathName)
	output, _, err := util.ExecCommandOutput(lsblkcommand, args)

	if err != nil {
		err = fmt.Errorf("unable to execute lsblk command, err=%s", err.Error())
		fmt.Printf(err.Error())
		return nil, err
	}

	if strings.Contains(output, failedDevPath) {
		err = fmt.Errorf("%s for %s", failedDevPath, dev.Pathname)
		return nil, err
	}

	return getPartitions(&output)
}

// process the output of the lsblkcommand command
func getPartitions(output *string) ([]model.DevicePartition, error) {
	log.Tracef("getPartition called with %+v", *output)
	r := regexp.MustCompile(lsblkPattern)
	out := r.FindAllString(*output, -1)
	devicePartitions := []model.DevicePartition{}

	for _, line := range out {
		if string(line) != "" && !strings.HasPrefix(string(line), "NAME") && !strings.Contains(string(line), "TYPE") {
			result := util.FindStringSubmatchMap(line, r)
			log.Tracef("deviceInfo result:%v", result)
			size, err := strconv.ParseInt(result["size"], 10, 64)
			if err != nil {
				return devicePartitions, err
			}
			if result["type"] == partType {
				devicePartition := &model.DevicePartition{
					Name:          result["name"],
					Partitiontype: result["type"],
					Size:          size,
				}
				devicePartitions = append(devicePartitions, *devicePartition)
			}

		}
	}
	return devicePartitions, nil
}

func isPartitionPresent(dev *model.Device) bool {
	log.Tracef(">>>>> isPartitionPresent called for device %+v", dev)
	defer log.Trace("<<<<< isPartitionPresent")

	devicePartitionInfos, _ := GetPartitionInfo(dev)
	if devicePartitionInfos != nil && len(devicePartitionInfos) != 0 {
		return true
	}
	return false
}

func cleanPartitions(dev *model.Device) error {
	log.Tracef(">>>>> cleanPartitions called for device %+v", dev)
	defer log.Trace("<<<<< cleanPartitions")

	devicePartitionInfos, _ := GetPartitionInfo(dev)
	if devicePartitionInfos != nil && len(devicePartitionInfos) != 0 {
		for _, part := range devicePartitionInfos {
			args := []string{"remove", "--force", "--retry", part.Name}
			_, _, err := util.ExecCommandOutput(dmsetupcommand, args)
			if err != nil {
				return fmt.Errorf("failed to remove partition map for %s. Error: %s", part.Name, err.Error())
			}
		}
	}
	return nil
}
