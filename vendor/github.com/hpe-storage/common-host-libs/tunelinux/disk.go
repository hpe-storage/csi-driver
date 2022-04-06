package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
)

const (
	// UdevFilePathName path name of the udev file deployed to tune settings
	UdevFilePathName = "/etc/udev/rules.d/99-nimble-tune.rules"
	udevAttrFormat   = "ATTR{queue/%s}=\"%s\""
)

var (
	// UdevTemplatePath is the path to template 99-nimble-tune.rules file
	UdevTemplatePath = GetUdevTemplateFile()
)

// GetUdevTemplateFile returns udev template file path if bundled with NLT accordingly
func GetUdevTemplateFile() (configFile string) {
	if util.GetNltHome() != "" {
		// path bundled with NLT
		return util.GetNltHome() + "nimbletune/99-nimble-tune.rules"
	}
	// path if nimbletune is delivered as separate utility
	return "./99-nimble-tune.rules"
}

func getBlockParamRecommendation(param string, value string, recommended string) (recommendation string, status string) {
	log.Trace("getBlockParamRecommendation called with param ", param, " value ", value, " recommended ", recommended)
	if recommended == value {
		recommendation = value
		status = ComplianceStatus.String(Recommended)
	} else {
		recommendation = recommended
		status = ComplianceStatus.String(NotRecommended)
	}
	return recommendation, status
}

// getBlockQueueRecommendation obtains recommendation for given block queue parameter and value
func getBlockQueueRecommendation(device *model.Device, param string, value string, deviceType string) (setting *Recommendation, err error) {
	log.Trace("getBlockQueueRecommendation called")
	var optionSetting *Recommendation
	var recommendation string
	var status string
	blockParamMap, _ := getParamToTemplateFieldMap(Disk, "recommendation", "")
	blockParamDescriptionMap, _ := getParamToTemplateFieldMap(Disk, "description", "")
	blockParamSeverityMap, _ := getParamToTemplateFieldMap(Disk, "severity", "")

	if param == "scheduler" {
		// scheduleMatcher extracts scheduler value as it appears as cfq [noop] deadline
		schedulerMatcher := regexp.MustCompile(".*\\[(?P<scheduler>.*)\\]")
		result := util.FindStringSubmatchMap(value, schedulerMatcher)
		scheduler := result["scheduler"]
		value = scheduler
	}
	// Get recommendation settings for given parameter and value obtained

	for index, dev := range blockParamMap {
		if dev.DeviceType == deviceType {
			recommendation, status = getBlockParamRecommendation(param, value, blockParamMap[index].deviceMap[param])
			optionSetting = &Recommendation{
				ID:              linux.HashMountID(param + device.AltFullPathName + device.SerialNumber),
				Category:        Category.String(Disk),
				Level:           blockParamSeverityMap[index].deviceMap[param],
				Description:     blockParamDescriptionMap[index].deviceMap[param],
				Parameter:       param,
				Value:           value,
				Recommendation:  recommendation,
				CompliantStatus: status,
				Device:          All,
				FsType:          "",
				Vendor:          "",
			}
		}
	}
	return optionSetting, nil

}

// GetBlockQueueRecommendations obtain block queue settings of the device
func GetBlockQueueRecommendations(device *model.Device, deviceType string) (settings []*Recommendation, err error) {
	log.Trace("GetBlockQueueRecommendations called with ", device.AltFullPathName)
	// Read block queue settings and keep adding recommendations
	params := []string{"add_random", "rq_affinity", "scheduler", "rotational", "nr_requests", "max_sectors_kb", "read_ahead_kb"}
	var dmQueueParamFormat = "/sys/block/dm-%s/queue/%s"
	var recommendations []*Recommendation

	for _, param := range params {
		fileName := fmt.Sprintf(dmQueueParamFormat, device.Minor, param)
		value, err := ioutil.ReadFile(fileName)
		if err != nil {
			log.Error("Unable to read param ", param, " from File ", fileName)
			continue
		}
		paramValue := strings.TrimRight(string(value), "\n")
		optionSetting, err := getBlockQueueRecommendation(device, param, string(paramValue), deviceType)
		if err != nil {
			log.Error("Unable to get block queue recommendation for ", device.AltFullPathName, " param ", param, " value ", string(value))
		}
		recommendations = append(recommendations, optionSetting)
	}
	return recommendations, nil
}

// GetDeviceRecommendations obtain various recommendations for block device settings on host
func GetDeviceRecommendations(devices []*model.Device, deviceParam ...string) (settings []*Recommendation, err error) {
	log.Trace("GetDeviceRecommendations called")
	var recommendations []*Recommendation
	var deviceRecommendations []*Recommendation

	// If no deviceType is passed, we assume Nimble as deviceType
	deviceType := "Nimble"
	if len(deviceParam) != 0 {
		deviceType = deviceParam[0]
	}

	err = loadTemplateSettings()
	if err != nil {
		return nil, err
	}
	for _, device := range devices {
		// get device specific recommendations
		deviceRecommendations, err = GetBlockQueueRecommendations(device, deviceType)
		if err != nil {
			log.Error("Unable to get recommendations for block device queue parameters ", err.Error())
			return nil, err
		}
		// append to overall recommendations
		recommendations = append(recommendations, deviceRecommendations...)
		// block queue settings will most likely be same for all devices. skip here
		break
	}
	return recommendations, nil
}

func updateUdevRule() (err error) {
	// Get all nimble devices
	devices, err := linux.GetLinuxDmDevices(true, util.GetVolumeObject("", ""))
	if err != nil {
		log.Error("Unable to get Nimble devices ", err.Error())
		return err
	}
	// Get current device recommendations
	recommendations, err := GetDeviceRecommendations(devices)
	if err != nil {
		return err
	}
	lines, err := util.FileGetStrings(UdevFilePathName)
	if err != nil {
		return err
	}

	// Update existing file with new recommendations
	for index, line := range lines {
		if strings.HasPrefix(line, "ATTR") {
			for _, recommendation := range recommendations {
				if strings.Contains(line, recommendation.Parameter) {
					// replace with recommended value
					log.Trace("updating block param ", recommendation.Parameter, " with value ", recommendation.Recommendation)
					lines[index] = fmt.Sprintf(udevAttrFormat, recommendation.Parameter, recommendation.Recommendation)
					// move to next attr in file
					break
				}
			}
		}
	}

	// Write recommended settings to file
	err = util.FileWriteStrings(UdevFilePathName, lines)
	return err
}

// SetBlockDeviceRecommendations set block queue param recommendations
func SetBlockDeviceRecommendations() (err error) {
	// Copy 99-nimble-tune.rules supplied with utility
	err = copyTemplateFile(UdevTemplatePath, UdevFilePathName)
	if err != nil {
		return err
	}
	// Update existing udev rule with new recommendations
	err = updateUdevRule()
	if err != nil {
		return err
	}
	// Reload UDEV rules
	err = linux.UdevadmReloadRules()
	if err != nil {
		return err
	}
	// Trigger UDEV rules
	err = linux.UdevadmTrigger()
	if err != nil {
		return err
	}
	log.Info("Successfully applied disk queue settings using udev")
	return nil
}
