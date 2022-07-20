package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"sync"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
)

// Category recommendation category type
type Category int

const (
	// Filesystem category type
	Filesystem = iota + 1
	// Disk category type
	Disk
	// Multipath category type
	Multipath
	// Fc category type
	Fc
	// Iscsi category type
	Iscsi
)

func (s Category) String() string {
	switch s {
	case Filesystem:
		return "filesystem"
	case Disk:
		return "disk"
	case Multipath:
		return "multipath"
	case Fc:
		return "fc"
	case Iscsi:
		return "iscsi"
	}
	return ""
}

// ComplianceStatus of the compliance
type ComplianceStatus int

const (
	// NotRecommended with recommended value
	NotRecommended ComplianceStatus = iota
	// Recommended with recommended value
	Recommended
)

func (s ComplianceStatus) String() string {
	switch s {
	case Recommended:
		return "recommended"
	case NotRecommended:
		return "not-recommended"
	}
	return ""
}

// Severity for the recommendation made
type Severity int

const (
	// Info Information level
	Info Severity = 1 + iota
	// Warning level
	Warning
	// Critical level
	Critical
	// Error level
	Error
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Critical:
		return "critical"
	case Error:
		return "error"
	}
	return ""
}

// Recommendation for the settings on the host
type Recommendation struct {
	// ID unique identifier for the recommendation
	ID string `json:"id,omitempty"`
	// Category recommendation category among (filesystem, multipath, device, iscsi, fc, system)
	Category string `json:"category,omitempty"`
	// Level severity level of the recommendation
	Level string `json:"severity,omitempty"`
	// Description brief description about the recommendation
	Description string `json:"description,omitempty"`
	// Parameter parameter name for recommendation
	Parameter string `json:"parameter,omitempty"`
	// Value current parameter value
	Value string `json:"value,omitempty"`
	// Recommendation recommended parameter value
	Recommendation string `json:"recommendation,omitempty"`
	// Status compliant status of the setting
	CompliantStatus string `json:"status,omitempty"`
	// Device device name
	Device string `json:"device,omitempty"`
	// MountPoint mount point for the device
	MountPoint string `json:"mountpoint,omitempty"`
	// FsType filesystem type, one among (xfs, ext2, ext3 and brtfs)
	FsType string `json:"fstype,omitempty"`
	// Vendor fibrechannel vendor name
	Vendor string `json:"vendor,omitempty"`
}

// TemplateSetting for the settings on the host
type TemplateSetting struct {
	// Category recommendation category among (filesystem, multipath, device, iscsi, fc, system)
	Category string `json:"category,omitempty"`
	// Level severity level of the recommendation
	Level string `json:"severity,omitempty"`
	// Description brief description about the recommendation
	Description string `json:"description,omitempty"`
	// Parameter parameter name for recommendation
	Parameter string `json:"parameter,omitempty"`
	// Recommendation recommended parameter value
	Recommendation string `json:"recommendation,omitempty"`
	// Driver driver module name
	Driver string `json:"driver,omitempty"`
}

type DeviceTemplate struct {
	DeviceType    string
	TemplateArray []TemplateSetting
}

type DeviceRecommendation struct {
	DeviceType    string
	RecomendArray []*Recommendation
}

type DeviceMap struct {
	DeviceType string
	deviceMap  map[string]string
}

var (
	// Global config of template settings
	deviceTemplate []DeviceTemplate
	// RW lock to prevent concurrent loading
	configLock = new(sync.RWMutex)
	// ConfigFile file path of template settings for recommendation.
	ConfigFile        = GetConfigFile()
	defaultDeviceType = "Nimble"
)

const (
	// NotApplicable recommendation attribute not applicable
	NotApplicable = "n/a"
	// All applicable to all devices
	All = "all"
)

// GetConfigFile returns config.json file path if bundled with NLT accordingly
func GetConfigFile() (configFile string) {
	pluginType := os.Getenv("PLUGIN_TYPE")
	if pluginType != "" {
		if pluginType == "cv" {
			// path bundled with cloud container plugins
			return "/opt/hpe-storage/nimbletune/cloud_vm_config.json"
		}
		// path bundled with on-prem container plugins
		return "/opt/hpe-storage/nimbletune/config.json"
	}

	if util.GetNltHome() != "" {
		// path bundled with NLT
		return util.GetNltHome() + "nimbletune/config.json"
	}
	// path if nimbletune is delivered as separate utility
	return "./config.json"
}

// SetConfigFile sets the config file path (overrides the default value)
func SetConfigFile(configFile string) {
	ConfigFile = configFile
}

// loadTemplateSettings synchronize and load configuration for template of recommended settings
// File read will only happen during initial load, so we take lock for complete loading.
func loadTemplateSettings() (err error) {
	configLock.Lock()
	defer configLock.Unlock()

	DeviceType := [2]string{"Nimble", "3PARdata"}
	for _, deviceType := range DeviceType {
		var configExist bool = false

		/* check if the initial device config is already read */
		/* TODO Find a more efficient way to check deviceType config exists */
		for _, devType := range deviceTemplate {
			if devType.DeviceType == deviceType {
				log.Tracef("%s device config exists. Skipping", deviceType)
				configExist = true
			}
		}

		// already loaded template settings from config file
		// once the initial config for deviceType is loaded, we should not repopulate it
		if !configExist {
			var devicetemplateSettings DeviceTemplate
			log.Tracef("Reading template config file: %s , for device: %s", ConfigFile, deviceType)

			file, err := ioutil.ReadFile(ConfigFile)
			if err != nil {
				log.Error("Template Config File read error: ", err.Error())
				return err
			}

			// Fetch device section from config.json
			var result map[string]interface{}
			err = json.Unmarshal(file, &result)

			if err != nil {
				log.Error("Template Config File Unmarshal error: ", err.Error())
				return err
			}
			deviceSection := result[deviceType]
			if deviceSection != nil {
				distroType, err := linux.GetDistro()
				if err != nil {
					log.Trace("Received error while fetching distroType , using default configuration ", err.Error())
					distroType = "Default"
				}

				log.Trace("Os DistroType ", distroType)
				config := (deviceSection.(map[string]interface{}))[distroType]
				if config == nil {
					log.Warningf("Distro section: %s , not present for deviceType: %s , using default config", distroType, deviceType)
					config = deviceSection.(map[string]interface{})["Default"] /* Default section is alays expected to present, hence no error check */
				}
				// TODO Add version specific parsing for the distroType
				switch config.(type) {
				case interface{}:
					for _, v := range config.([]interface{}) {
						var temp TemplateSetting
						convertmap := v.(map[string]interface{})
						temp.Category = convertmap["category"].(string)
						temp.Parameter = convertmap["parameter"].(string)
						temp.Recommendation = convertmap["recommendation"].(string)
						temp.Level = convertmap["severity"].(string)
						temp.Description = convertmap["description"].(string)

						if _, ok := convertmap["driver"]; ok {
							temp.Driver = convertmap["driver"].(string)
						}

						devicetemplateSettings.TemplateArray = append(devicetemplateSettings.TemplateArray, temp)
					}

				default:
					log.Tracef("%v is unknown \n ", config)
					return errors.New("error: while Parsing section for deviceType : " + deviceType)
				}

			} else {
				log.Tracef("DeviceType:  %s absent in config.json", deviceType)
				return errors.New("error: unable to get deviceType " + deviceType)
			}

			devicetemplateSettings.DeviceType = deviceType
			deviceTemplate = append(deviceTemplate, devicetemplateSettings)
		}
	}
	return nil
}

// get map of either parameter -> recommended value or param -> description fields for given category
func getParamToTemplateFieldMap(category Category, field string, driver string) ([]DeviceMap, error) {
	var deviceTypeMap []DeviceMap

	for _, dev := range deviceTemplate {
		var tempMap DeviceMap

		tempMap = DeviceMap{
			deviceMap: make(map[string]string),
		}

		for index := range dev.TemplateArray {

			if dev.TemplateArray[index].Category == Category.String(category) {
				if driver != "" && dev.TemplateArray[index].Driver != driver {
					// obtain only specific driver params in a given category
					continue
				}

				switch field {
				case "recommendation":
					// populate parameter -> recommended value map
					tempMap.deviceMap[dev.TemplateArray[index].Parameter] = dev.TemplateArray[index].Recommendation
				case "description":
					// populate parameter -> description map
					tempMap.deviceMap[dev.TemplateArray[index].Parameter] = dev.TemplateArray[index].Description
				case "severity":
					// populate parameter -> severity map
					tempMap.deviceMap[dev.TemplateArray[index].Parameter] = dev.TemplateArray[index].Level
				default:
					// populate parameter -> recommended value map by default
					tempMap.deviceMap[dev.TemplateArray[index].Parameter] = dev.TemplateArray[index].Recommendation
				}
			}
		}

		tempMap.DeviceType = dev.DeviceType
		deviceTypeMap = append(deviceTypeMap, tempMap)
	}

	return deviceTypeMap, nil
}

// copyTemplateFile copies the template config files supplied with the tool as destination file
func copyTemplateFile(srcFile string, destFile string) (err error) {
	// Copy the multipath.conf supplied with utility
	args := []string{srcFile, destFile}
	out, _, err := util.ExecCommandOutput("cp", args)
	if err != nil {
		log.Error("Unable to create file ", destFile, ", ", err.Error())
		return errors.New("error: unable to create " + destFile + " before applying settings, reason: " + err.Error() + out)
	}
	return nil
}

func appendRecommendations(current []*Recommendation, final []*Recommendation) (recommendations []*Recommendation, err error) {
	// Append all current recommendations to the final list
	if current != nil {
		for _, recommendation := range current {
			final = append(final, recommendation)
		}
	}
	return final, nil
}

// GetRecommendations obtain various recommendations for non-compliant settings on host
func GetRecommendations(deviceParam ...string) (settings []*Recommendation, err error) {
	log.Trace(">>>>> GetRecommendations")
	defer log.Trace("<<<<< GetRecommendations")

	// Obtain recommendations for each category
	var recommendations []*Recommendation
	var fileSystemRecommendations []*Recommendation
	var deviceRecommendations []*Recommendation
	var iscsiRecommendations []*Recommendation
	var fcRecommendations []*Recommendation

	deviceType := defaultDeviceType
	// If no deviceType is passed, we assume Nimble as deviceType
	if len(deviceParam) != 0 {
		deviceType = deviceParam[0]
	}

	// Get all devices
	devices, err := linux.GetLinuxDmDevices(false, util.GetVolumeObject("", ""))
	if err != nil {
		log.Error("Unable to get devices ", err.Error())
		return nil, err
	}
	// Get filesystem recommendations
	fileSystemRecommendations, err = GetFileSystemRecommendations(devices)
	if err != nil {
		log.Error("Unable to get FS recommendations ", err.Error())
		return nil, err
	}
	// get the appended final list
	recommendations, _ = appendRecommendations(fileSystemRecommendations, recommendations)

	// Get block device recommendations
	deviceRecommendations, err = GetDeviceRecommendations(devices, deviceType)
	if err != nil {
		log.Error("Unable to get device recommendations ", err.Error())
		return nil, err
	}
	// get the appended final list
	recommendations, _ = appendRecommendations(deviceRecommendations, recommendations)

	if _, err = os.Stat(linux.IscsiConf); os.IsNotExist(err) == false {
		// Get iscsi recommendations
		iscsiRecommendations, err = GetIscsiRecommendations(deviceType)
		if err != nil {
			log.Error("Unable to get iscsi recommendations ", err.Error())
			return nil, err
		}
		// get the appended final list
		recommendations, _ = appendRecommendations(iscsiRecommendations, recommendations)
	}

	// Get multipath recommendations
	multipathRecommendations, err := GetMultipathRecommendations()
	if err != nil {
		log.Error("Unable to get multipath recommendations ", err.Error())
		return nil, err
	}
	// get the appended final list
	for _, mRecomendation := range multipathRecommendations {
		if mRecomendation.DeviceType == deviceType {
			recommendations, _ = appendRecommendations(mRecomendation.RecomendArray, recommendations)
		}
	}

	// Get multipath recommendations
	fcRecommendations, err = GetFcRecommendations()
	if err != nil {
		log.Error("Unable to get fc recommendations ", err.Error())
		// Might not be an FC system. continue with other recommendations
	}
	// get the appended final list
	recommendations, _ = appendRecommendations(fcRecommendations, recommendations)

	return recommendations, nil
}

// SetRecommendations set recommendations for all categories except filesystem and fc
func SetRecommendations(global bool) (err error) {
	err = SetMultipathRecommendations()
	if err != nil {
		return errors.New("unable to set multipath recommendations, error: " + err.Error())
	}
	err = SetBlockDeviceRecommendations()
	if err != nil {
		return errors.New("unable to set device recommendations, error: " + err.Error())
	}
	err = SetIscsiRecommendations(global)
	if err != nil {
		return errors.New("unable to set iscsi recommendations, error: " + err.Error())
	}
	return nil
}

// isNcmRunning check if NCM is running
func isNcmRunning() (isRunning bool, err error) {
	isRunning = false
	if _, err = os.Stat("/etc/ncm.conf"); err != nil {
		return isRunning, err
	}
	args := []string{"nlt", "status"}
	_, _, err = util.ExecCommandOutput("service", args)
	if err == nil {
		isRunning = true
	}
	return isRunning, nil
}
