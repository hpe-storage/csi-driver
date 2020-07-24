package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"encoding/json"
	"errors"
	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
	"io/ioutil"
	"os"
	"sync"
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

var (
	// Global config of template settings
	templateSettings []TemplateSetting
	// RW lock to prevent concurrent loading
	configLock = new(sync.RWMutex)
	// ConfigFile file path of template settings for recommendation.
	ConfigFile = GetConfigFile()
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
// TODO: make this optional only when configfile option is provided. Use in-memory settings
func loadTemplateSettings() (err error) {
	configLock.Lock()
	defer configLock.Unlock()
	if len(templateSettings) != 0 {
		// already loaded template settings from config file
		// once the initial config is loaded, we should return most of the time from here
		return nil
	}
	log.Traceln("Reading template config file:", ConfigFile)
	file, err := ioutil.ReadFile(ConfigFile)
	if err != nil {
		log.Error("Template Config File read error: ", err.Error())
		return err
	}
	err = json.Unmarshal(file, &templateSettings)
	if err != nil {
		log.Error("Template Config File Unmarshall error: ", err.Error())
		return err
	}
	return nil
}

// get map of either parameter -> recommended value or param -> description fields for given category
func getParamToTemplateFieldMap(category Category, field string, driver string) (paramTemplateFieldMap map[string]string, err error) {
	paramTemplateFieldMap = make(map[string]string)
	for index := range templateSettings {
		if templateSettings[index].Category == Category.String(category) {
			if driver != "" && templateSettings[index].Driver != driver {
				// obtain only specific driver params in a given category
				continue
			}
			switch field {
			case "recommendation":
				// populate parameter -> recommended value map
				paramTemplateFieldMap[templateSettings[index].Parameter] = templateSettings[index].Recommendation
			case "description":
				// populate parameter -> description map
				paramTemplateFieldMap[templateSettings[index].Parameter] = templateSettings[index].Description
			case "severity":
				// populate parameter -> severity map
				paramTemplateFieldMap[templateSettings[index].Parameter] = templateSettings[index].Level
			default:
				// populate parameter -> recommended value map by default
				paramTemplateFieldMap[templateSettings[index].Parameter] = templateSettings[index].Recommendation
			}
		}
	}
	return paramTemplateFieldMap, nil
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
func GetRecommendations() (settings []*Recommendation, err error) {
	log.Trace(">>>>> GetRecommendations")
	defer log.Trace("<<<<< GetRecommendations")

	// Obtain recommendations for each category
	var recommendations []*Recommendation
	var fileSystemRecommendations []*Recommendation
	var deviceRecommendations []*Recommendation
	var iscsiRecommendations []*Recommendation
	var multipathRecommendations []*Recommendation
	var fcRecommendations []*Recommendation

	// Get all nimble devices
	devices, err := linux.GetLinuxDmDevices(false, util.GetVolumeObject("", ""))
	if err != nil {
		log.Error("Unable to get Nimble devices ", err.Error())
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
	deviceRecommendations, err = GetDeviceRecommendations(devices)
	if err != nil {
		log.Error("Unable to get device recommendations ", err.Error())
		return nil, err
	}
	// get the appended final list
	recommendations, _ = appendRecommendations(deviceRecommendations, recommendations)

	if _, err = os.Stat(linux.IscsiConf); os.IsNotExist(err) == false {
		// Get iscsi recommendations
		iscsiRecommendations, err = GetIscsiRecommendations()
		if err != nil {
			log.Error("Unable to get iscsi recommendations ", err.Error())
			return nil, err
		}
		// get the appended final list
		recommendations, _ = appendRecommendations(iscsiRecommendations, recommendations)
	}

	// Get multipath recommendations
	multipathRecommendations, err = GetMultipathRecommendations()
	if err != nil {
		log.Error("Unable to get multipath recommendations ", err.Error())
		return nil, err
	}
	// get the appended final list
	recommendations, _ = appendRecommendations(multipathRecommendations, recommendations)

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
