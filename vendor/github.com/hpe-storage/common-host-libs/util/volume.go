// Copyright 2020 Hewlett Packard Enterprise Development LP

package util

import (
	"encoding/json"
	"github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
)

func GetVolumeObject(serialNumber, lunID string) *model.Volume {
	logger.Tracef(">>>>> GetVolumeObject %s, %s", serialNumber, lunID)
	defer logger.Tracef("<<<< GetVolumeObject")
	var volObj *model.Volume
	volObj = &model.Volume{}
	volObj.SerialNumber = serialNumber
	volObj.LunID = lunID

	return volObj
}

func GetSecondaryArrayLUNIds(details string) []int32 {
	logger.Tracef(">>>>> GetSecondaryArrayLUNIds %s", details)
	defer logger.Tracef("<<<< GetSecondaryArrayLUNIds")
	var secondaryArrayDetails model.SecondaryBackendDetails
	logger.Tracef("\n About to unmarshal %s", details)
	err := json.Unmarshal([]byte(details), &secondaryArrayDetails)
	if err != nil {
		logger.Errorf("\n Error in GetSecondaryArrayLUNIds %s", err.Error())
		return []int32{}
	}
	numberOfSecondaryBackends := len(secondaryArrayDetails.PeerArrayDetails)
	var secondaryLunIds []int32 = make([]int32, numberOfSecondaryBackends)
	for i := 0; i < numberOfSecondaryBackends; i++ {
		secondaryLunIds[i] = secondaryArrayDetails.PeerArrayDetails[i].LunID
	}
	return secondaryLunIds
}

func GetSecondaryArrayTargetNames(details string) []string {
	logger.Tracef(">>>>> GetSecondaryArrayTargetNames %s", details)
	defer logger.Tracef("<<<< GetSecondaryArrayTargetNames")
	var secondaryArrayDetails model.SecondaryBackendDetails
	logger.Tracef("\n About to unmarshal %s", details)
	err := json.Unmarshal([]byte(details), &secondaryArrayDetails)
	if err != nil {
		logger.Errorf("\n Error in GetSecondaryArrayTargetNames %s", err.Error())
		return []string{}
	}
	numberOfSecondaryBackends := len(secondaryArrayDetails.PeerArrayDetails)
	var secondaryTargetNames []string
	for i := 0; i < numberOfSecondaryBackends; i++ {
		for _, targetNameRetrieved := range secondaryArrayDetails.PeerArrayDetails[i].TargetNames {
			secondaryTargetNames = append(secondaryTargetNames, targetNameRetrieved)
		}
	}
	return secondaryTargetNames
}

func GetSecondaryArrayDiscoveryIps(details string) []string {
	logger.Tracef(">>>>> GetSecondaryArrayDiscoveryIps %s", details)
	defer logger.Tracef("<<<< GetSecondaryArrayDiscoveryIps")
	var secondaryArrayDetails model.SecondaryBackendDetails
	logger.Tracef("\n About to unmarshal %s", details)
	err := json.Unmarshal([]byte(details), &secondaryArrayDetails)
	if err != nil {
		logger.Errorf("\n Error in GetSecondaryArrayDiscoveryIps %s", err.Error())
		return []string{}
	}
	numberOfSecondaryBackends := len(secondaryArrayDetails.PeerArrayDetails)
	var secondaryDiscoverIps []string
	for i := 0; i < numberOfSecondaryBackends; i++ {
		for _, discoveryIpRetrieved := range secondaryArrayDetails.PeerArrayDetails[i].DiscoveryIPs {
			secondaryDiscoverIps = append(secondaryDiscoverIps, discoveryIpRetrieved)
		}
	}
	return secondaryDiscoverIps
}

func GetSecondaryBackends(details string) []*model.SecondaryLunInfo {
	logger.Tracef(">>>>> GetSecondaryBackends %s", details)
	defer logger.Tracef("<<<< GetSecondaryBackends")
	var secondaryArrayDetails model.SecondaryBackendDetails
	logger.Tracef("\n About to unmarshal %s", details)
	err := json.Unmarshal([]byte(details), &secondaryArrayDetails)
	if err != nil {
		logger.Errorf("\n Error in GetSecondaryBackends %s", err.Error())
		logger.Errorf("\n Passed details %s", details)
		return nil
	}
	return secondaryArrayDetails.PeerArrayDetails

}
