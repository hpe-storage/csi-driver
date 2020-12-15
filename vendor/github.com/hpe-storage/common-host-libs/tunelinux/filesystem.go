package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
)

var (
	fsOptionMap = map[string]bool{
		"discard":   false,
		"nobarrier": false,
		"_netdev":   false}

	fsOptionDescriptionMap = map[string]string{
		"discard":   "Enable discard option to allow auto space reclamation using UNMAP",
		"nobarrier": "Enable nobarrier to disable write barriers on SSD drives",
		"_netdev":   "Enable _netdev for iSCSI mount devices to allow network to be up before mounting device"}
)

// getDiscardRecommendation : get template for recommendation related to discard option for filesystem
func getRecommendationByFsOption(mountPoint *model.Mount, option string, currentStatus string, recommended bool) (recommendation *Recommendation) {
	var optionSetting *Recommendation
	var recommendedStatus = "enabled" // set enabled status by default
	var complianceStatus string
	var description = fsOptionDescriptionMap[option]

	// ignore _netdev option for FC devices
	if option == "_netdev" && mountPoint.Device.IscsiTargets == nil {
		return nil
	}
	// ignore discard option for ext3 based filesystems as its not supported
	if option == "discard" {
		fsType, err := linux.GetFsType(*mountPoint)
		if err != nil {
			// device might got unmounted, ignore the recommenation
			return nil
		} else if fsType == linux.FsType.String(linux.Ext3) {
			return nil
		}
	}

	if recommended && currentStatus == "enabled" {
		complianceStatus = ComplianceStatus.String(Recommended)
	} else if recommended && currentStatus == "disabled" {
		// missing option case
		complianceStatus = ComplianceStatus.String(NotRecommended)
	} else {
		// invalid option present but not recommended
		recommendedStatus = "disabled"
		complianceStatus = ComplianceStatus.String(NotRecommended)
	}

	optionSetting = &Recommendation{
		ID:              linux.HashMountID(option + mountPoint.Device.AltFullPathName + mountPoint.Device.SerialNumber),
		Category:        Category.String(Filesystem),
		Level:           Severity.String(Warning),
		Description:     description,
		Parameter:       option,
		Value:           currentStatus,
		Recommendation:  recommendedStatus,
		CompliantStatus: complianceStatus,
		Device:          mountPoint.Device.AltFullPathName,
		FsType:          "", // TODO identify filesystem type
		Vendor:          "",
	}
	log.Trace("Option settings for ", option)
	log.Trace(optionSetting.Category, optionSetting.Parameter, optionSetting.Value, optionSetting.Recommendation, optionSetting.Description)
	return optionSetting
}

// getRecommendationForMountDevice : obtain recommendations based on mount options for the device
func getRecommendationForMountDevice(mountPoint *model.Mount) (settings []*Recommendation, err error) {
	log.Trace("getRecommendationForMountDevice called with ", mountPoint.Device.AltFullPathName)
	var options []string
	var recommendations []*Recommendation
	var recommendation *Recommendation

	// get options used to mount the device
	options, err = linux.GetMountOptionsForDevice(mountPoint.Device)
	if err != nil {
		log.Error("Unable to get mount options for device ", mountPoint.Device.AltFullPathName, err.Error())
		return nil, err
	}

	//now we got mount options for the device. verify and create recommendations
	for index := range options {
		_, present := fsOptionMap[options[index]]
		if present == true {
			// option is recommended and enabled, set value as true to indicate this option is set.
			fsOptionMap[options[index]] = true
			recommendation = getRecommendationByFsOption(mountPoint, options[index], "enabled", true)
			if recommendation != nil {
				// append recommendation for each option
				recommendations = append(recommendations, recommendation)
			}
		}
	}

	// now get the recommendations for missing options, i.e options with value as false (not set)
	for option, value := range fsOptionMap {
		if value == false {
			// missing option case, indicate this is needed
			recommendation = getRecommendationByFsOption(mountPoint, option, "disabled", true)
			if recommendation != nil {
				// append recommendation for each option
				recommendations = append(recommendations, recommendation)
			}
		}
	}

	log.Trace("total recommendations for mount device ", len(recommendations))
	return recommendations, nil
}

// GetFileSystemRecommendations obtain various recommendations for filesystems mounted on host
func GetFileSystemRecommendations(devices []*model.Device) (settings []*Recommendation, err error) {
	log.Trace(">>>>> GetFilesystemRecommendations")
	defer log.Trace("<<<<< GetFileSystemRecommendations")

	//var recommendations []*Recommendation
	var deviceRecommendations []*Recommendation
	var recommendations []*Recommendation

	// Get all mounts from nimble devices
	mounts, err := linux.GetMountPointsForDevices(devices)
	if err != nil {
		log.Error("Unable to get mount points for devices " + err.Error())
		return nil, err
	}

	for _, mountPoint := range mounts {
		// get device specific recommendations
		deviceRecommendations, err = getRecommendationForMountDevice(mountPoint)
		// append to overall recommendations
		if deviceRecommendations != nil {
			for _, recommendation := range deviceRecommendations {
				recommendations = append(recommendations, recommendation)
			}
		}
	}
	return recommendations, err
}
