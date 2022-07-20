package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
)

// HbaVendor indicates the vendor type of the Fibre Channel HBA
type HbaVendor int

const (
	// Emulex emulex hba adapter type which also indicates lpfc driver
	Emulex HbaVendor = 1 + iota
	// Qlogic qlogic hba adapter type which also indicates qla2xxx/qla4xxx driver
	Qlogic
	// Brocade brocade hba adapter type which also indicates bfa driver
	Brocade
	// Cisco cisco converged adapter type which also indicates fnic driver
	Cisco
)

func (vendor HbaVendor) String() string {
	switch vendor {
	case Emulex:
		return "emulex"
	case Qlogic:
		return "qlogic"
	case Brocade:
		return "brocade"
	case Cisco:
		return "cisco"
	}
	return ""
}

// HbaDriver indicates the driver name of the Fibre Channel HBA
type HbaDriver int

const (
	// Qla2xxx qlogic hba driver type
	Qla2xxx HbaDriver = 1 + iota
	// Qla4xxx qlogic hba driver
	Qla4xxx
	// Lpfc emulex hba driver type
	Lpfc
	// Bfa brocade hba driver type
	Bfa
	// Fnic cisco hba driver type
	Fnic
)

func (vendor HbaDriver) String() string {
	switch vendor {
	case Qla2xxx:
		return "qla2xxx"
	case Qla4xxx:
		return "qla4xxx"
	case Lpfc:
		return "lpfc"
	case Bfa:
		return "bfa"
	case Fnic:
		return "fnic"
	}
	return ""
}

// getFcAdapterType obtain the FC hba vendor type
func getFcAdapterType() (hbaVendor HbaVendor, moduleName string, err error) {
	log.Trace("getFcAdapterType called")
	var sysModulePath = "/sys/module/%s/"

	if _, err = os.Stat(fmt.Sprintf(sysModulePath, HbaDriver.String(Lpfc))); err == nil {
		log.Trace("Emulex lpfc driver found")
		return Emulex, HbaDriver.String(Lpfc), err
	} else if _, err = os.Stat(fmt.Sprintf(sysModulePath, HbaDriver.String(Qla2xxx))); err == nil {
		log.Trace("Qlogic qla2xxx driver found")
		return Qlogic, HbaDriver.String(Qla2xxx), err
	} else if _, err = os.Stat(fmt.Sprintf(sysModulePath, HbaDriver.String(Qla4xxx))); err == nil {
		log.Trace("Qlogic qla4xxx driver found")
		return Qlogic, HbaDriver.String(Qla4xxx), err
	} else if _, err = os.Stat(fmt.Sprintf(sysModulePath, HbaDriver.String(Bfa))); err == nil {
		log.Trace("Brocade bfa driver found")
		return Brocade, HbaDriver.String(Bfa), err
	} else if _, err = os.Stat(fmt.Sprintf(sysModulePath, HbaDriver.String(Fnic))); err == nil {
		log.Trace("Cisco fnic driver found")
		return Cisco, HbaDriver.String(Fnic), err
	}
	return Emulex, "unknown", errors.New("unknown FC driver")
}

// getFcParamRecommendation obtain recommendation for give lpfc param and value
func getFcParamRecommendation(module string, paramName string, currentValue string, recommendedValue string, description string, severity string) (setting *Recommendation, err error) {
	log.Trace("getFcParamRecommendation called")
	var vendor string
	var optionSetting *Recommendation
	if currentValue == recommendedValue {
		optionSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(Recommended),
		}
	} else {
		optionSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(NotRecommended),
		}
	}

	switch module {
	case HbaDriver.String(Lpfc):
		vendor = HbaVendor.String(Emulex)
	case HbaDriver.String(Qla2xxx):
		vendor = HbaVendor.String(Qlogic)
	case HbaDriver.String(Qla4xxx):
		vendor = HbaVendor.String(Qlogic)
	case HbaDriver.String(Bfa):
		vendor = HbaVendor.String(Brocade)
	case HbaDriver.String(Fnic):
		vendor = HbaVendor.String(Cisco)
	default:
		log.Trace("invalid module passed for recommendation ", module)
		return nil, errors.New("invalid module passed for recommendation " + module)
	}

	// set common attributes
	optionSetting.ID = linux.HashMountID(module + paramName)
	optionSetting.Category = Category.String(Fc)
	optionSetting.Level = severity
	optionSetting.Description = description
	optionSetting.Parameter = paramName
	optionSetting.Value = currentValue
	optionSetting.Recommendation = recommendedValue
	optionSetting.Device = All
	optionSetting.FsType = ""
	optionSetting.Vendor = vendor

	return optionSetting, nil
}

// getFcModuleParamRecommendations return the recommendations for lpfc driver parameters
func getFcModuleParamRecommendations(module string) (settings []*Recommendation, err error) {
	log.Trace("getFcModuleParamRecommendations called with module ", module)
	var recommendation *Recommendation
	var recommendations []*Recommendation

	// load template recommendations from config file
	err = loadTemplateSettings()
	if err != nil {
		return nil, err
	}

	paramMap, _ := getParamToTemplateFieldMap(Fc, "recommendation", module)
	paramDescriptionMap, _ := getParamToTemplateFieldMap(Fc, "description", module)
	paramSeverityMap, _ := getParamToTemplateFieldMap(Fc, "severity", module)

	for index, dev := range paramMap {
		if dev.DeviceType == defaultDeviceType {
			for key, value := range paramMap[index].deviceMap {
				path := fmt.Sprintf("/sys/module/%s/parameters/%s", module, key)
				_, err = os.Stat(path)
				if err != nil {
					log.Trace("parameter path not found for module ", module, " ", key)
					return nil, err
				}
				out, _ := ioutil.ReadFile(path)
				currentValue := string(out)
				description := paramDescriptionMap[index].deviceMap[key]
				severity := paramSeverityMap[index].deviceMap[key]
				recommendation, err = getFcParamRecommendation(module, key, strings.TrimRight(currentValue, "\n"), value, description, severity)
				if err != nil {
					log.Trace("parameter recommendation failed for module ", module, " ", key)
					return nil, err
				}
				recommendations = append(recommendations, recommendation)
			}
		}
	}
	return recommendations, err
}

// getFcAdapterParamRecommendations obtains recommendations for FC adapter parameter values
func getFcAdapterParamRecommendations(module string) (settings []*Recommendation, err error) {
	log.Trace("getFcAdapterParamRecommendations called with module ", module)
	var recommendations []*Recommendation
	recommendations, err = getFcModuleParamRecommendations(module)
	if err != nil {
		log.Trace("Unable to get recommendations for FC module parameters for ", module)
		return nil, err
	}
	return recommendations, err
}

// GetFcRecommendations obtain various recommendations for FC adapter settings on host
func GetFcRecommendations() (settings []*Recommendation, err error) {
	log.Trace("GetFcRecommendations called")
	var recommendations []*Recommendation

	var isVM bool
	// identify if running as a virtual machine
	isVM, err = linux.IsVirtualMachine()
	if err != nil {
		log.Error("unable to determine if system is running as a virtual machine ", err.Error())
		return nil, err
	}
	if isVM {
		log.Error("system is running as a virtual machine. Skipping FC recommendations")
		return nil, err
	}

	// identify adapter type
	_, module, err := getFcAdapterType()
	if err != nil {
		log.Error("Unable to find FC adpter type ", err.Error())
		return nil, err
	}

	recommendations, err = getFcAdapterParamRecommendations(module)
	if err != nil {
		log.Error("Unable to get FC adapter param recommendations ", err.Error())
		return nil, err
	}
	return recommendations, err
}
