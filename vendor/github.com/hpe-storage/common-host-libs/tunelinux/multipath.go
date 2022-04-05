package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"errors"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/mpathconfig"
	"github.com/hpe-storage/common-host-libs/util"
)

const (
	multipath = "multipath"

	// multipath params
	multipathParamPattern = "\\s*(?P<name>.*?)\\s+(?P<value>.*)"
)

var (
	deviceBlockPattern = map[string]string{
		"Nimble": "(?s)devices\\s+{\\s*.*device\\s*{(?P<device_block>.*Nimble.*?)}",
		"3par":   "(?s)devices\\s+{\\s*.*device\\s*{(?P<device_block>.*3PAR.*?)}",
	}
)

// GetMultipathConfigFile returns path of the template multipath.conf file according to OS distro
func GetMultipathTemplateFile() (configFile string, err error) {
	log.Traceln(">>>>> GetMultipathTemplateFile")
	defer log.Traceln("<<<<< GetMultipathTemplateFile")

	// assume current directory by default
	configPath := "./"
	// assume generic config by default
	multipathConfig := "multipath.conf.generic"

	// get config base path
	if util.GetNltHome() != "" {
		// path bundled with NLT
		configPath = util.GetNltHome() + "nimbletune/"
	}

	// get os distro to determine approprite multipath settings
	osInfo, err := linux.GetOsInfo()
	if err != nil {
		return "", err
	}
	major, err := strconv.Atoi(osInfo.GetOsMajorVersion())
	if err != nil {
		return "", err
	}

	switch osInfo.GetOsDistro() {
	// Ubuntu 18 settings are latest
	case linux.OsTypeUbuntu:
		if major >= 18 {
			multipathConfig = "multipath.conf.upstream"
		}
	// RHEL/CentOS 8 settings are latest
	case linux.OsTypeRedhat:
		fallthrough
	case linux.OsTypeCentos:
		if major >= 8 {
			multipathConfig = "multipath.conf.upstream"
		}
	}

	log.Tracef("using multipath template file as %s", configPath+multipathConfig)
	return configPath + multipathConfig, nil
}

// getMultipathDeviceParamRecommendation returns recommendation for given param of device section in multipath.conf
func getMultipathDeviceParamRecommendation(paramKey string, currentValue string, recommendedValue string, description string, severity string) (recommendation *Recommendation, err error) {
	log.Trace("getMultipathDeviceParamRecommendation called with paramKey ", paramKey, " value ", currentValue, " recommended ", recommendedValue)
	var optionSetting *Recommendation

	// create recommendation
	if currentValue == recommendedValue || strings.Replace(currentValue, "\"", "", 2) == recommendedValue {
		optionSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(Recommended),
		}
	} else {
		optionSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(NotRecommended),
		}
	}
	// set common attributes
	optionSetting.ID = linux.HashMountID("multipath" + "device" + paramKey)
	optionSetting.Category = Category.String(Multipath)
	optionSetting.Level = severity
	optionSetting.Description = description
	optionSetting.Parameter = paramKey
	optionSetting.Value = currentValue
	optionSetting.Recommendation = recommendedValue
	optionSetting.Device = All
	optionSetting.FsType = ""
	optionSetting.Vendor = ""

	return optionSetting, nil
}

// getMultipathDeviceScopeRecommendations obtain recommendations for block section of multipath.conf
func getMultipathDeviceScopeRecommendations(deviceBlock string) (settings []DeviceRecommendation, err error) {
	log.Trace("getMultipathDeviceScopeRecommendations called")
	var recommendation *Recommendation
	var deviceRecommendations []DeviceRecommendation
	var keyFound bool
	var paramValue string
	var paramKey string

	deviceBlockRecommendationMap, _ := getParamToTemplateFieldMap(Multipath, "recommendation", "")
	deviceSettingsDescriptionMap, _ := getParamToTemplateFieldMap(Multipath, "description", "")
	deviceSettingsSeverityMap, _ := getParamToTemplateFieldMap(Multipath, "severity", "")

	// get individual parameters from device block
	currentSettings := strings.Split(string(deviceBlock), "\n")

	for index, _ := range deviceBlockRecommendationMap {

		var currRecommendation DeviceRecommendation
		for key := range deviceBlockRecommendationMap[index].deviceMap {

			keyFound = false
			for _, setting := range currentSettings {
				if setting != "" {
					r := regexp.MustCompile(multipathParamPattern)
					// extract key value from parameter string
					if r.MatchString(setting) {
						result := util.FindStringSubmatchMap(setting, r)
						paramKey = result["name"]
						paramValue = result["value"]
					} else {
						log.Error("Invalid multipath device param value for recommendation ", setting)
						continue
					}
					if paramKey == key {
						// found the matching key for recommended parameter in /etc/multipath.conf
						keyFound = true
						break
					}
				}
			}
			var description = deviceSettingsDescriptionMap[index].deviceMap[key]
			var recommendedValue = deviceBlockRecommendationMap[index].deviceMap[key]
			var severity = deviceSettingsSeverityMap[index].deviceMap[key]
			if keyFound == true {
				log.Info(" Keyfound = ", keyFound)
				// entry found in /etc/multipath.conf
				recommendation, err = getMultipathDeviceParamRecommendation(paramKey, strings.TrimSpace(paramValue), recommendedValue, description, severity)
				if err != nil {
					log.Error("Unable to get recommendation for multipath param", paramKey, "value ", paramValue)
					continue
				}
			} else {
				// missing needed parameters in /etc/multipath.conf
				recommendation, err = getMultipathDeviceParamRecommendation(key, "", recommendedValue, description, severity)
				if err != nil {
					log.Error("Unable to get recommendation for multipath param", paramKey, "value ", paramValue)
					continue
				}
			}
			currRecommendation.RecomendArray = append(currRecommendation.RecomendArray, recommendation)
		}
		currRecommendation.DeviceType = deviceBlockRecommendationMap[index].DeviceType
		deviceRecommendations = append(deviceRecommendations, currRecommendation)
	}
	return deviceRecommendations, nil
}

// IsMultipathRequired returns if multipath needs to be enabled on the system
func IsMultipathRequired() (required bool, err error) {
	var isVM bool
	var multipathRequired = true
	// identify if running as a virtual machine and guest iscsi is enabled
	isVM, err = linux.IsVirtualMachine()
	if err != nil {
		log.Error("unable to determine if system is running as a virtual machine ", err.Error())
		return false, err
	}
	if isVM && !IsIscsiEnabled() {
		log.Error("system is running as a virtual machine and guest iSCSI is not enabled. Skipping multipath recommendations")
		multipathRequired = false
	}
	return multipathRequired, nil
}

// GetMultipathRecommendations obtain various recommendations for multipath settings on host
func GetMultipathRecommendations() (settings []DeviceRecommendation, err error) {
	log.Trace("GetMultipathRecommendations called")
	var deviceRecommendations []DeviceRecommendation

	var isMultipathRequired bool

	// check if multipath is required in the first place on the system
	isMultipathRequired, err = IsMultipathRequired()
	if err != nil {
		log.Error("unable to determine if multipath is required ", err.Error())
		return nil, err
	}
	if !isMultipathRequired {
		log.Info("multipath is not required on the system, skipping get multipath recommendations")
		return nil, nil
	}
	// load config settings
	err = loadTemplateSettings()
	if err != nil {
		return nil, err
	}

	for _, devicePattern := range deviceBlockPattern {
		// Check if /etc/multipath.conf present
		if _, err = os.Stat(linux.MultipathConf); os.IsNotExist(err) {
			log.Error("/etc/multipath.conf file missing")
			// Generate All Recommendations By default
			deviceRecommendations, err = getMultipathDeviceScopeRecommendations("")
			if err != nil {
				log.Error("Unable to get recommendations for multipath device settings ", err.Error())
			}
			return deviceRecommendations, err
		}
		// Obtain contents of /etc/multipath.conf
		content, err := ioutil.ReadFile(linux.MultipathConf)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}

		r := regexp.MustCompile(devicePattern)
		if r.MatchString(string(content)) {
			// found Device block
			result := util.FindStringSubmatchMap(string(content), r)
			deviceBlock := result["device_block"]
			deviceRecommendations, err = getMultipathDeviceScopeRecommendations(strings.TrimSpace(deviceBlock))
			if err != nil {
				log.Error("Unable to get recommendations for multipath device settings ", err.Error())
			}
		} else {
			// Device section missing.
			// Generate All Recommendations By default
			deviceRecommendations, err = getMultipathDeviceScopeRecommendations("")
			if err != nil {
				log.Error("Unable to get recommendations for multipath device settings ", err.Error())
			}
		}
	}

	return deviceRecommendations, err
}

// setMultipathRecommendations sets device scope recommendations in multipath.conf
func setMultipathRecommendations(recommendations []*Recommendation, device string) (err error) {
	var devicesSection *mpathconfig.Section
	var deviceSection *mpathconfig.Section
	var defaultsSection *mpathconfig.Section
	// parse multipath.conf into different sections and apply recommendation
	config, err := mpathconfig.ParseConfig(linux.MultipathConf)
	if err != nil {
		return err
	}

	deviceSection, err = config.GetDeviceSection(device)
	if err != nil {
		devicesSection, err = config.GetSection("devices", "")
		if err != nil {
			// Device section is not found, get or create devices{} and then add device{} section
			devicesSection, err = config.AddSection("devices", config.GetRoot())
			if err != nil {
				return errors.New("Unable to add new devices section")
			}
		}
		deviceSection, err = config.AddSection("device", devicesSection)
		if err != nil {
			return errors.New("Unable to add new nimble device section")
		}
	}
	// update recommended values in device section
	for _, recommendation := range recommendations {
		deviceSection.GetProperties()[recommendation.Parameter] = recommendation.Recommendation
	}

	// update find_multipaths as no if set in defaults section
	defaultsSection, err = config.GetSection("defaults", "")
	if err != nil {
		// add a defaults section with override for find_multipaths
		defaultsSection, err = config.AddSection("defaults", config.GetRoot())
		if err != nil {
			return errors.New("Unable to add new defaults section in /etc/multipath.conf")
		}
	}
	if err == nil {
		// if we find_multipaths key with yes value or if the key is absent (in case of Ubuntu)
		// set it to no
		value := (defaultsSection.GetProperties())["find_multipaths"]
		if value == "yes" || value == "" {
			(defaultsSection.GetProperties())["find_multipaths"] = "no"
		}
	}

	// save modified configuration
	err = mpathconfig.SaveConfig(config, linux.MultipathConf)
	if err != nil {
		return err
	}

	return nil
}

// SetMultipathRecommendations sets multipath.conf settings
func SetMultipathRecommendations() (err error) {
	log.Traceln(">>>>> SetMultipathRecommendations")
	defer log.Traceln("<<<<< SetMultipathRecommendations")

	// Take a backup of existing multipath.conf
	f, err := os.Stat(linux.MultipathConf)

	if err != nil || f.Size() == 0 {
		multipathTemplate, err := GetMultipathTemplateFile()
		if err != nil {
			return err
		}
		// Copy the multipath.conf supplied with utility
		err = util.CopyFile(multipathTemplate, linux.MultipathConf)
		if err != nil {
			return err
		}
	}
	// Get current recommendations
	recommendations, err := GetMultipathRecommendations()
	if err != nil {
		return err
	}
	if len(recommendations) == 0 {
		log.Warning("no recommendations found for multipath.conf settings")
		return nil
	}

	// Apply new recommendations for mismatched values
	for _, dev := range recommendations {
		err = setMultipathRecommendations(dev.RecomendArray, dev.DeviceType)
	}
	if err != nil {
		return err
	}
	// Start service as it would have failed to start initially if multipath.conf is missing
	err = linux.ServiceCommand(multipath, "start")
	if err != nil {
		return err
	}

	// Reconfigure settings in any case to make sure new settings are applied
	_, err = linux.MultipathdReconfigure()
	if err != nil {
		return err
	}
	log.Info("Successfully configured multipath.conf settings")
	return nil
}

// ConfigureMultipath ensures following
// 1. Service is enabled and running
// 2. Multipath settings are configured correctly
func ConfigureMultipath() (err error) {
	log.Traceln(">>>>> ConfigureMultipath")
	defer log.Traceln("<<<<< ConfigureMultipath")

	// Ensure multipath.conf settings
	err = SetMultipathRecommendations()
	if err != nil {
		return err
	}
	return nil
}
