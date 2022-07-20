package tunelinux

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"errors"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
)

const (
	// iscsi params
	queueDepthPattern             = "^node.session.queue_depth\\s*=\\s*(?P<queue_depth>.*)"
	cmdsMaxPattern                = "^node.session.cmds_max\\s*=\\s*(?P<cmds_max>.*)"
	nrSessionsPattern             = "^node.session.nr_sessions\\s*=\\s*(?P<nr_sessions>.*)"
	replacementTimeoutPattern     = "^node.session.timeo.replacement_timeout\\s*=\\s*(?P<replacement_timeout>.*)"
	loginTimeoutPattern           = "^node.conn\\[0\\].timeo.login_timeout\\s*=\\s*(?P<login_timeout>.*)"
	noopOutTimeoutPattern         = "^node.conn\\[0\\].timeo.noop_out_timeout\\s*=\\s*(?P<noop_out_timeout>.*)"
	noopOutTimeoutIntervalPattern = "^node.conn\\[0\\].timeo.noop_out_interval\\s*=\\s*(?P<noop_out_interval>.*)"
	startupPattern                = "^node.startup\\s*=\\s*(?P<startup>.*)"
	iscsi                         = "iscsi"
)

var (
	// used for applying settings on logged-in sessions
	iscsiParamFormatMap = map[string]string{
		"startup":             "node.startup",
		"queue_depth":         "node.session.queue_depth",
		"cmds_max":            "node.session.cmds_max",
		"login_timeout":       "node.conn[0].timeo.login_timeout",
		"noop_out_timeout":    "node.conn[0].timeo.noop_out_timeout",
		"noop_out_interval":   "node.conn[0].timeo.noop_out_interval",
		"replacement_timeout": "node.session.timeo.replacement_timeout",
		"nr_sessions":         "node.session.nr_sessions"}

	// used to verify parameter settings in iscisd.conf
	iscsiParamPatternMap = map[string]string{
		"startup":             startupPattern,
		"queue_depth":         queueDepthPattern,
		"cmds_max":            cmdsMaxPattern,
		"login_timeout":       loginTimeoutPattern,
		"noop_out_timeout":    noopOutTimeoutPattern,
		"noop_out_interval":   noopOutTimeoutIntervalPattern,
		"replacement_timeout": replacementTimeoutPattern,
		"nr_sessions":         nrSessionsPattern}
)

// getIscsiParamRecommendation get the recommendation for given parameter and value
func getIscsiParamRecommendation(param string, recommendedValue string, currentLine string, description string, severity string) (recommendation *Recommendation, err error) {
	log.Trace("getIscsiParamRecommendation called")
	var pattern string
	var currentValue string
	var optionSetting *Recommendation

	// obtain pattern for given parameter name to match iscsid.conf settings
	if _, present := iscsiParamPatternMap[param]; present == true {
		pattern = iscsiParamPatternMap[param]
	} else {
		// not a valid parameter
		log.Error("not a valid iscsid param for recommendation ", param)
		return nil, errors.New("invalid iscsi param passed")
	}

	r := regexp.MustCompile(pattern)
	if r.MatchString(currentLine) {
		result := util.FindStringSubmatchMap(currentLine, r)
		currentValue = strings.TrimSpace(result[param])
	} else {
		// no param match with the text
		return nil, nil
	}

	// create recommendation
	if currentValue == recommendedValue {
		optionSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(Recommended),
		}
	} else {
		optionSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(NotRecommended),
		}
	}
	// set common attributes
	optionSetting.Category = Category.String(Iscsi)
	optionSetting.ID = linux.HashMountID(param)
	optionSetting.Description = description
	optionSetting.Level = severity
	optionSetting.Parameter = param
	optionSetting.Value = currentValue
	optionSetting.Recommendation = recommendedValue
	optionSetting.Device = All
	optionSetting.FsType = ""
	optionSetting.Vendor = ""

	return optionSetting, nil
}

// getIscsiBindingRecommendation returns recommendation for given param of device section in multipath.conf
func getIscsiBindingRecommendation(currentValue string, recommendedValue string) (recommendation *Recommendation) {
	log.Trace("getMultipathDeviceParamRecommendation called with value ", currentValue, " recommended ", recommendedValue)
	var ifaceSetting *Recommendation

	// create recommendation
	if currentValue == recommendedValue {
		ifaceSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(Recommended),
		}
	} else {
		ifaceSetting = &Recommendation{
			CompliantStatus: ComplianceStatus.String(NotRecommended),
		}
	}
	// set common attributes
	ifaceSetting.ID = linux.HashMountID("iscsiportbinding")
	ifaceSetting.Category = Category.String(Iscsi)
	ifaceSetting.Level = Severity.String(Critical)
	ifaceSetting.Description = "iscsi port binding is required only when all interfaces are in same network. Enabling portbinding with mixed subnets causes iscsi login timeouts and delay in discovery"
	ifaceSetting.Parameter = "iscsi_port_binding"
	ifaceSetting.Value = currentValue
	ifaceSetting.Recommendation = recommendedValue
	ifaceSetting.Device = All

	return ifaceSetting
}

// getIfaceRecommendation returns recommendations for iscsi port binding
func getIfaceRecommendation() (recommendation *Recommendation, err error) {
	var networkAddressCountMap = make(map[string]int)
	ifaces, err := linux.GetIfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		// identify mismatch between network subnets of all ifaces
		networkAddress, err := linux.GetIPV4NetworkAddress(iface.NetworkInterface.AddressV4, iface.NetworkInterface.MaskV4)
		if err != nil {
			return nil, err
		}
		if _, present := networkAddressCountMap[networkAddress]; present {
			networkAddressCountMap[networkAddress]++
		} else {
			networkAddressCountMap[networkAddress] = 1
		}

		if len(networkAddressCountMap) > 1 {
			// invalid configuration, ifaces are bound to networks in different subnet
			recommendation = getIscsiBindingRecommendation("enabled", "disabled")
			return recommendation, nil
		}
	}
	return nil, nil
}

// IsIscsiEnabled return if iSCSI services are installed and enabled on the system
func IsIscsiEnabled() (enabled bool) {
	var iScsiEnabled = true
	// check if conf file is present
	// TODO add service checks as well for open-iscsi/iscsid
	if _, err := os.Stat(linux.IscsiConf); os.IsNotExist(err) {
		log.Error("/etc/iscsi/iscsid.conf file missing. assuming sw iscsi is not enabled")
		return false
	}
	return iScsiEnabled
}

// GetIscsiRecommendations obtain various recommendations for iSCSI settings on host
func GetIscsiRecommendations(deviceParam ...string) (settings []*Recommendation, err error) {
	log.Trace("GetIscsiRecommendations called")

	// check if iscsi services are installed and enabled
	if !IsIscsiEnabled() {
		log.Info("iSCSI services are not enabled on the host. Ignoring get recommendations")
		return nil, nil
	}

	var recommendation *Recommendation
	var recommendations []*Recommendation
	err = loadTemplateSettings()
	if err != nil {
		return nil, err
	}
	iscsiParamMap, _ := getParamToTemplateFieldMap(Iscsi, "recommendation", "")
	iscsiParamDescriptionMap, _ := getParamToTemplateFieldMap(Iscsi, "description", "")
	iscsiParamSeverityMap, _ := getParamToTemplateFieldMap(Iscsi, "severity", "")

	// If no deviceType is passed, we assume Nimble as deviceType
	deviceType := "Nimble"
	if len(deviceParam) != 0 {
		deviceType = deviceParam[0]
	}

	configLock.Lock()
	defer configLock.Unlock()
	content, err := ioutil.ReadFile(linux.IscsiConf)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	lines := strings.Split(string(content), "\n")
	// get recommendation for each param in the map matching with the iscsid.conf param

	for index, dev := range iscsiParamMap {
		if dev.DeviceType == deviceType {
			for param, recommendedValue := range iscsiParamMap[index].deviceMap {
				formattedParam, _ := getIscsiFormattedParam(param)
				for _, line := range lines {
					if false == strings.HasPrefix(line, formattedParam) {
						// skip other parameters in iscsid.conf
						continue
					}
					var description = iscsiParamDescriptionMap[index].deviceMap[param]
					var severity = iscsiParamSeverityMap[index].deviceMap[param]
					recommendation, err = getIscsiParamRecommendation(param, recommendedValue, line, description, severity)
					if err != nil {
						log.Error("Unable to get recommendation for iscsi param ", param, "error: ", err.Error())
					}
					if recommendation != nil {
						recommendations = append(recommendations, recommendation)
					}
					break
				}
			}
		}
	}
	// ignore errors during iface recommendations
	ifaceRecommendation, err := getIfaceRecommendation()
	if ifaceRecommendation != nil {
		recommendations = append(recommendations, ifaceRecommendation)
	}
	return recommendations, nil
}

func readIscsiConfigFile() (content []byte, err error) {
	configLock.Lock()
	defer configLock.Unlock()
	// Obtain contents of /etc/iscsi/iscsid.conf
	content, err = ioutil.ReadFile(linux.IscsiConf)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	return content, nil
}

func writeIscsiConfigFile(content []byte) (err error) {
	// acquire lock to prevent concurrent writes to config file
	configLock.Lock()
	defer configLock.Unlock()
	// write content to iscsid.conf file
	err = ioutil.WriteFile(linux.IscsiConf, content, 0600)
	if err != nil {
		return err
	}
	return nil
}

// SetIscsiParamRecommendation set parameter value in iscsid.conf
func SetIscsiParamRecommendation(parameter string, recommendation string) (err error) {
	log.Trace("SetIscsiParamRecommendation called with ", parameter, " ", recommendation)
	var paramFound = false

	// get formatted parameter name for iscsid.conf
	formattedParam, err := getIscsiFormattedParam(parameter)
	if err != nil {
		return err
	}
	// check if conf file is present
	if _, err = os.Stat(linux.IscsiConf); os.IsNotExist(err) {
		log.Error(linux.IscsiConf, " file missing")
		return err
	}
	content, err := readIscsiConfigFile()
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, formattedParam) {
			// update recommended value inline
			lines[index] = formattedParam + " = " + recommendation + "\n"
			paramFound = true
			break
		}
	}
	// check if parameter is found and update with recommendation after backup is taken
	if paramFound == true {
		output := strings.Join(lines, "\n")
		err = writeIscsiConfigFile([]byte(output))
		if err != nil {
			log.Error("Unable to modify iscsi parameter ", parameter, " error: ", err.Error())
			return err
		}
	}
	log.Trace("Successfully updated iscsid.conf param ", parameter, " value: ", recommendation)
	return nil
}

// getIscsiFormattedParam get properly formatted string for given parameter name
func getIscsiFormattedParam(parameter string) (formattedParam string, err error) {
	err = nil
	if _, present := iscsiParamFormatMap[parameter]; present == true {
		formattedParam = iscsiParamFormatMap[parameter]
	} else {
		err = errors.New("invalid parameter passed to get iscsi formatted string " + parameter)
		log.Error("Unable to get iscsi parameter format for ", parameter, " error: ", err.Error())
	}
	return formattedParam, err
}

func updateIscsiSessionParameters(remediations []*Recommendation) (err error) {
	var formattedParam string
	// Get logged-in iSCSi sessions
	iscsiTargets, err := linux.GetLoggedInIscsiTargets()
	if err != nil {
		log.Error("Unable to get logged-in iscsi session to update recommendations, error: ", err.Error())
		return err
	}
	// Update parameters for logged-in sessions
	for _, remediation := range remediations {
		for _, iscsiTarget := range iscsiTargets {
			formattedParam, err = getIscsiFormattedParam(remediation.Parameter)
			if err != nil {
				log.Error("Unable to get formatted param for ", remediation.Parameter, " target: ", iscsiTarget, "error: ", err.Error())
				// continue with other remediations
				continue
			}
			err = SetIscsiSessionParam(iscsiTarget, formattedParam, remediation.Recommendation)
			if err != nil {
				log.Error("Unable to update iscsi session param ", remediation.Parameter, " target: ", iscsiTarget, "error: ", err.Error())
				// continue with other remediations
				continue
			}
			log.Info("Successfully updated iscsi session param ", remediation.Parameter, " target: ", iscsiTarget)
		}
	}
	return nil
}

// SetIscsiSessionParam set parameter value for logged-in iscsi session
func SetIscsiSessionParam(target string, parameter string, value string) (err error) {
	log.Trace("SetIscsiSessionParam called with ", target, " param: ", parameter, " value: ", value)
	args := []string{"--mode", "node", "--op", "update", "-T", target, "--name", parameter, "--value", value}
	_, _, err = util.ExecCommandOutput("iscsiadm", args)
	if err != nil {
		err = errors.New("unable to set iSCSI param value for " + parameter + " value " + value + "error: " + err.Error())
		return err
	}
	return err
}

// SetIscsiRecommendations set iscsi param recommendations
func SetIscsiRecommendations(global bool) (err error) {
	log.Trace("SetIscsiRecommendations called")
	var remediations []*Recommendation
	// Get iSCSI recommendations
	recommendations, err := GetIscsiRecommendations()
	if err != nil {
		log.Error("Unable to get current recommendations to configure ", err.Error())
		return err
	}
	// Figure out not-recommended settings
	for _, recommendation := range recommendations {
		if recommendation.CompliantStatus == ComplianceStatus.String(NotRecommended) {
			if recommendation.Parameter == "nr_sessions" {
				if ncmRunning, _ := isNcmRunning(); ncmRunning == true {
					// ignore nr_sessions if NCM is running as it will again trim down the sessions.
					log.Error("Unable to tune nr_sessions in iscsid.conf as NCM is running. Please change min_sessions_per_array in /etc/ncm.conf instead and restart nlt service")
					continue
				}
			}

			if global {
				// Modify iscsid.conf settings accordingly only when global param is not set
				err = SetIscsiParamRecommendation(recommendation.Parameter, recommendation.Recommendation)
				if err != nil {
					// continue with other recommendations
					continue
				}
			}
			// append to remediations list for corrected settings
			remediations = append(remediations, recommendation)
		}
	}
	if len(remediations) > 0 {
		// update remediations for logged-in sessions
		err = updateIscsiSessionParameters(remediations)
		if err != nil {
			return err
		}
		log.Info("Successfully set iSCSI recommendations on host")
	} else {
		log.Info("No further iSCSI recommendations are found for this host")
	}
	return nil
}

// ConfigureIscsi verifies and install necessary iSCSI packages if not present. It also makes sure if service is running.
func ConfigureIscsi() (err error) {
	log.Traceln(">>>>> ConfigureIscsi")
	defer log.Traceln("<<<<< ConfigureIscsi")

	// Init OS info
	_, err = linux.GetOsInfo()
	if err != nil {
		return err
	}

	// Make sure service is enabled on every reboot
	err = linux.EnableService(iscsi)
	if err != nil {
		return err
	}

	// Set best practice settings
	err = SetIscsiRecommendations(true)
	if err != nil {
		return err
	}

	// Start service
	err = linux.ServiceCommand(iscsi, "start")
	if err != nil {
		return err
	}

	return nil
}
