package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
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
	mountMutex              sync.Mutex
	umountMutex             sync.Mutex
	staleDeviceRemovalMutex sync.Mutex
	readProcMountsMutex     sync.Mutex
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

func GetMultipathDevices() (multipathDevices []model.MultipathDevice, err error) {
	log.Tracef(">>>> getMultipathDevices ")
	defer log.Trace("<<<<< getMultipathDevices")

	out, _, err := util.ExecCommandOutput("multipathd", []string{"show", "multipaths", "json"})

	if err != nil {
		return nil, fmt.Errorf("Failed to get the multipath devices due to the error: %s", err.Error())
	}

	if out != "" {
		multipathJson := new(model.MultipathInfo)
		err = json.Unmarshal([]byte(out), multipathJson)
		if err != nil {
			return nil, fmt.Errorf("Invalid JSON output of multipathd command: %s", err.Error())
		}

		for _, mapItem := range multipathJson.Maps {
			if len(mapItem.Vend) > 0 && isSupportedDeviceVendor(linux.DeviceVendorPatterns, mapItem.Vend) {
				if mapItem.Paths < 1 && mapItem.PathFaults > 0 {
					mapItem.IsUnhealthy = true
					log.Tracef("Known unhealthy multipath device: %s", mapItem.Name)
				}
				multipathDevices = append(multipathDevices, mapItem)
				continue
			}

			// residual non-functional multipath devices
			if len(mapItem.PathGroups) == 0 && mapItem.Queueing == "off" && mapItem.Features == "0" {
				mapItem.IsUnhealthy = true
				log.Tracef("Unknown orphan multipath device without path_groups: %s", mapItem.Name)
				multipathDevices = append(multipathDevices, mapItem)
			}
		}
		log.Infof("Found %d multipath devices %+v", len(multipathDevices), multipathDevices)
		return multipathDevices, nil
	}
	return nil, fmt.Errorf("Invalid multipathd command output received")
}

func isSupportedDeviceVendor(deviceVendors []string, vendor string) bool {
	for _, value := range deviceVendors {
		if value == vendor {
			return true
		}
	}
	return false
}

func RemoveBlockDevicesOfMultipathDevices(device model.MultipathDevice) error {
	log.Trace(">>>> RemoveBlockDevicesOfMultipathDevices")
	defer log.Trace("<<<<< RemoveBlockDevicesOfMultipathDevices")

	blockDevices := getBlockDevicesOfMultipathDevice(device)
	if len(blockDevices) == 0 {
		log.Infof("No block devices were found for the multipath device %s", device.Name)
		return nil
	}
	log.Infof("%d block devices found for the multipath device %s", len(blockDevices), device.Name)
	err := removeBlockDevices(blockDevices, device.Name)
	if err != nil {
		log.Errorf("Error occurred while removing the block devices of the  multipath device %s", device.Name)
		return err
	}
	log.Infof("Block devices of the multipath device %s are removed successfully.", device.Name)
	return nil
}

func getBlockDevicesOfMultipathDevice(device model.MultipathDevice) (blockDevices []string) {
	log.Tracef(">>>> getBlockDevicesOfMultipathDevice: %+v", device)
	defer log.Trace("<<<<< getBlockDevicesOfMultipathDevice")

	if len(device.PathGroups) > 0 {
		for _, pathGroup := range device.PathGroups {
			if len(pathGroup.Paths) > 0 {
				for _, path := range pathGroup.Paths {
					blockDevices = append(blockDevices, path.Dev)
				}
			}
		}
	}
	return blockDevices
}

func removeBlockDevices(blockDevices []string, multipathDevice string) error {
	log.Trace(">>>> removeBlockDevices: ", blockDevices)
	defer log.Trace("<<<<< removeBlockDevices")
	for _, blockDevice := range blockDevices {
		log.Debugf("Removing the block device %s of the multipath device %s", blockDevice, multipathDevice)
		cmd := exec.Command("sh", "-c", "echo 1 > /sys/block/"+blockDevice+"/device/delete")
		err := cmd.Run()
		if err != nil {
			log.Errorf("Error occurred while deleting the block device %s of the multipath device %s: %s", blockDevice, multipathDevice, err.Error())
			return err
		}
	}
	return nil
}

func UnmountMultipathDevice(multipathDevice string) error {
	log.Tracef(">>>> UnmountMultipathDevice: %s", multipathDevice)
	defer log.Trace("<<<<< UnmountMultipathDevice")

	mountPoints, err := findMountPointsOfMultipathDevice("/dev/mapper/" + multipathDevice)
	if err != nil {
		return fmt.Errorf("Error occurred while fetching the mount points of the multipath device %s:%s", multipathDevice, err.Error())
	}
	if len(mountPoints) == 0 {
		log.Infof("No mount points found for the multipath device %s", multipathDevice)
		return nil
	}

	for _, mountPoint := range mountPoints {
		err = unmount(mountPoint)
		if err != nil {
			log.Errorf("Error occurred while unmounting the mount point %s: %s", mountPoint, err.Error())
			return err
		}
		log.Debugf("Mount point %s unmounted successfully.", mountPoint)
	}
	return nil
}

func unmount(mountPoint string) error {
	log.Tracef("Unmount the mount point %s", mountPoint)

	umountMutex.Lock()
	defer umountMutex.Unlock()

	args := []string{mountPoint}
	_, rc, err := util.ExecCommandOutput("umount", args)
	if err != nil || rc != 0 {
		log.Errorf("Error occurred while unmounting the mount point %s: %s", mountPoint, err.Error())
		return err
	}
	return nil
}

func parseMounts() (mounts []model.ProcMount, err error) {
	log.Tracef(">>>> parseMounts")
	defer log.Trace("<<<<< parseMounts")
	readProcMountsMutex.Lock()
	defer readProcMountsMutex.Unlock()
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/mounts: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue // Ignore invalid lines
		}

		mounts = append(mounts, model.ProcMount{
			Device:     fields[0],
			MountPoint: fields[1],
			FileSystem: fields[2],
			Options:    fields[3],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read /proc/mounts: %w", err)
	}

	return mounts, nil
}

func findMountPointsOfMultipathDevice(multipathDevice string) (mountPoints []string, err error) {
	log.Tracef(">>>> findMountPointsOfMultipathDevice: %s", multipathDevice)
	defer log.Trace("<<<<< findMountPointsOfMultipathDevice")

	procMounts, err := parseMounts()
	if err != nil {
		log.Errorf("Error while getting the mount points of the device %s", multipathDevice)
		return mountPoints, err
	}
	for _, mount := range procMounts {
		if mount.Device == multipathDevice {
			if !strings.HasPrefix(mount.MountPoint, "/host/") {
				mountPoints = append(mountPoints, mount.MountPoint)
			}
		}
	}

	return mountPoints, nil
}

func RemoveMultipathDevice(multipathDevice string) error {
	log.Tracef(">>>> RemoveMultipathDevice: %s", multipathDevice)
	defer log.Trace("<<<<< RemoveMultipathDevice")

	staleDeviceRemovalMutex.Lock()
	defer staleDeviceRemovalMutex.Unlock()

	//check if the multipath device exists
	log.Infof("Checking whether the multipath device %s exists or not.", multipathDevice)
	if multipathDeviceExists(multipathDevice) {
		_, _, err := util.ExecCommandOutput("dmsetup", []string{"remove", multipathDevice})
		if err != nil {
			log.Errorf("Error occurred while removing the multipath device %s: %s", multipathDevice, err.Error())
			return err
		}
		log.Debugf("Multipath device %s is removed successfully.", multipathDevice)
	}
	log.Infof("Multipath device %s is already removed.", multipathDevice)
	return nil
}

func multipathDeviceExists(multipathDevice string) bool {
	log.Tracef(">>>> multipathDeviceExists: %s", multipathDevice)
	out, _, err := util.ExecCommandOutput("dmsetup", []string{"table"})
	if err != nil {
		log.Errorf("Unable to get the multipath devices information using dmsetup table command: %s", err.Error())
		return false
	}
	if strings.Contains(out, multipathDevice) {
		return true
	}
	return false
}

func forceDeleteMultipathDevice(multipathDevice string) error {
	log.Tracef(">>>> forceDeleteMultipathDevice: %s", multipathDevice)
	defer log.Trace("<<<<< forceDeleteMultipathDevice")

	_, _, err := util.ExecCommandOutput("dmsetup", []string{"remove", "-f", multipathDevice})
	if err != nil {
		log.Errorf("Error occurred while removing the multipath device %s by force: %s", multipathDevice, err.Error())
		return err
	}
	return nil
}
