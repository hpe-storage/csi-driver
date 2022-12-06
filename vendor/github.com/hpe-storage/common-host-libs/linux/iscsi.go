// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
	ping "github.com/sparrc/go-ping"
)

const (
	iscsicmd                = "iscsiadm"
	lscmd                   = "ls"
	iscsiadmPattern         = "(?m)^(?P<address>.*):(?P<port>\\d*),(?P<tag>\\d*) (?P<target>iqn.*(REPLACE_VENDOR):.*)$"
	byPathTargetPattern     = "^ip-(?P<address>.*):(?P<port>\\d*)-iscsi-(?P<target>iqn.*(REPLACE_VENDOR):.*)-lun"
	diskbypath              = "/dev/disk/by-path"
	ifacePath               = "/var/lib/iscsi/ifaces"
	altIfacePath            = "/etc/iscsi/ifaces"
	iscsiHostPathFormat     = "/sys/class/iscsi_host/"
	ifaceNetNamePattern     = "^iface.net_ifacename\\s*=\\s*(?P<network>.*)"
	hostDeviceFormat        = "/sys/class/scsi_host/host%s/device/"
	sessionIDPattern        = "session(?P<sessionid>\\d+)"
	hostTargetNameFormat    = "/sys/class/scsi_host/host%s/device/session%s/iscsi_session/session%s/targetname"
	hostTagNameFormat       = "/sys/class/scsi_host/host%s/device/session%s/iscsi_session/session%s/tpgt"
	hostTargetAddressFormat = "/sys/class/iscsi_host/host%s/device/session%s/connection%s:0/iscsi_connection/connection%s:0/persistent_address"
	hostTargetPortFormat    = "/sys/class/iscsi_host/host%s/device/session%s/connection%s:0/iscsi_connection/connection%s:0/port"
	successfulDot           = "successful."
	chapUserPattern         = "^\\s*node.session.auth.username\\s*=\\s*(?P<username>.*)\\s*$"
	chapPasswordPattern     = "^\\s*node.session.auth.password\\s*=\\s*(?P<password>.*)\\s*$"
	chapAuthPattern         = "^\\s*node.session.auth.authmethod\\s*=\\s*(?P<password>.*)\\s*$"
	validChapPasswordChars  = "WXaM1NbSTcdA453BCefyzDVwxIYZEFghijkRUlmG78HnopPQqrsKLtFu90vDEJO26+_)(*^%$#@!"
	// IscsiConf is configuration file for iscsid daemon
	IscsiConf               = "/etc/iscsi/iscsid.conf"
	chapPasswordMinLen      = 12
	chapPasswordMaxLen      = 16
	nodeChapUser            = "node.session.auth.username"
	nodeChapPassword        = "node.session.auth.password"
	iscsiHostScanPathFormat = "/sys/class/scsi_host/%s/scan"
	alreadyPresent          = "already present"
	endPointNotConnected    = "transport endpoint is not connected"
	PingCount               = 5
	PingInterval            = 10 * time.Millisecond
	PingTimeout             = 5 * time.Second
	DefaultIscsiPort        = 3260
	iscsiSessionDir         = "/sys/class/iscsi_session"
)

var (
	iscsiMutex           sync.Mutex
	targetVendorPatterns = []string{"com.nimblestorage", "com.3pardata", "org.truenas.ctl", "org.freenas.ctl"}
)

//type of Scope (volume, group)
type targetScope int

const (
	// VolumeScope scope of the device is volume
	VolumeScope targetScope = 1
	// GroupScope scope of the device is group
	GroupScope targetScope = 2
)

func (e targetScope) String() string {
	switch e {
	case VolumeScope:
		return "volume"
	case GroupScope:
		return "group"
	default:
		return "" // default targetScope is empty which is treated at volume scope
	}
}

// return iscsiadmPattern with vendor replacement eg. (com.nimblestorage|com.3pardata)
// "(?m)^(?P<address>.*):(?P<port>\\d*),(?P<tag>\\d*) (?P<target>iqn.*(REPLACE_VENDORNAME):.*)$"
func getIscsiadmPattern() string {
	vendorPattern := strings.Join(targetVendorPatterns, "|")
	return strings.Replace(iscsiadmPattern, "REPLACE_VENDOR", vendorPattern, -1)
}

// return byPathTargetPattern with vendor replacement eg. (com.nimblestorage|com.3pardata)
// "^ip-(?P<address>.*):(?P<port>\\d*)-iscsi-(?P<target>iqn.*(REPLACE_VENDORNAME):.*)-lun"
func getByPathTargetPattern() string {
	vendorPattern := strings.Join(targetVendorPatterns, "|")
	return strings.Replace(byPathTargetPattern, "REPLACE_VENDOR", vendorPattern, -1)
}

// getReachableDiscoveryPortals returns list of target portals which are reachable from given input
// if provided portals are virtual IP's(i.e virtualPortal is true) then only single reachable VIP will be returned
func getReachableDiscoveryPortals(discoveryIPs []string, virtualPortal bool) (reachablePortals []string, err error) {
	for _, discoveryIP := range discoveryIPs {
		// determine if this is reachable
		isPortalReachable, err := isReachable("", discoveryIP)
		if err != nil {
			log.Warnf("unable to determine if target portal %s is reachable, ignoring this for discovery", discoveryIP)
			continue
		}
		if isPortalReachable {
			reachablePortals = append(reachablePortals, discoveryIP)
			if virtualPortal {
				// if virtual portal ip then one reachable entry is sufficient for discovery
				return reachablePortals, nil
			}
		}
	}
	return reachablePortals, nil
}

func updateChapForLoggedInTargets(volume *model.Volume) {
	log.Tracef(">>>>> updateChapForLoggedInTargets for volume %s", volume.SerialNumber)
	defer log.Tracef("<<<<< updateChapForLoggedInTargets")
	loggedInTargets, _ := GetIscsiNodesFromIsciadm()
	var chapUser, chapPassword string
	if volume.Chap == nil {
		chapUser = ""
		chapPassword = ""
	} else {
		chapUser = volume.Chap.Name
		chapPassword = volume.Chap.Password
	}
	for _, targetName := range volume.TargetNames() {
		for _, loggedInTarget := range loggedInTargets {
			// update only the targets matching the target iqn
			if loggedInTarget.Name == targetName {
				log.Tracef("updating chapUser for Targets :%s Portal :%s to %s", loggedInTarget.Name, loggedInTarget.Address, chapUser)
				// update chapuser irrespective (could be a toggle case chap->empty)
				err := updateChapUser(loggedInTarget, chapUser)
				if err != nil {
					// log and proceed for other targets
					log.Errorf(err.Error())
				}
				err = updateChapPassword(loggedInTarget, chapPassword)
				if err != nil {
					// log and proceed for other targets
					log.Errorf(err.Error())
				}
			}
		}
	}
}

// areTargetsLoggedIn returns true if all specified targets are logged-in
func areTargetsLoggedIn(requiredTargets []string) (bool, error) {
	loggedInTargets, err := GetLoggedInIscsiTargets()
	if err != nil {
		return false, fmt.Errorf("unable to determine already logged-in targets, err %s", err.Error())
	}
	for _, requiredTarget := range requiredTargets {
		found := false
		for _, loggedInTarget := range loggedInTargets {
			if loggedInTarget == requiredTarget {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func loginToVolume(volume *model.Volume) (err error) {
	log.Tracef(">>>>> loginToVolume for volume %s, lun %s", volume.SerialNumber, volume.LunID)
	defer log.Tracef("<<<<< loginToVolume")

	// get candidates for discovery
	var reachablePortals []string
	if len(volume.TargetNames()) > 1 {
		// if multiple targets for single volume, then fetch all reachable discovery portals
		reachablePortals, _ = getReachableDiscoveryPortals(volume.DiscoveryIPs, false)
	} else {
		reachablePortals, _ = getReachableDiscoveryPortals(volume.DiscoveryIPs, true)
	}

	if len(reachablePortals) == 0 {
		return fmt.Errorf("none of the discovery portals provided [%+v] are reachable", volume.DiscoveryIPs)
	}
	// perform discovery and login to targets
	discoveredTargets, err := PerformDiscovery(reachablePortals)
	if err != nil {
		return err
	}

	// Get iscsi ifaces bound to network interfaces
	ifaces, err := GetIfaces()
	// treat iface path not found error as no ifaces bound
	if err != nil && !os.IsNotExist(err) {
		log.Errorf("unable to retrieve iSCSI bound ifaces. Error: %s", err.Error())
		return fmt.Errorf("unable to retrieve iSCSI bound ifaces. Error: %s", err.Error())
	}

	// login to all targets for given volume

	for _, target := range volume.TargetNames() {
		if volume.Chap == nil {
			err = loginToTarget(discoveredTargets, target, ifaces, "", "", volume.ConnectionMode)
		} else {
			err = loginToTarget(discoveredTargets, target, ifaces, volume.Chap.Name, volume.Chap.Password, volume.ConnectionMode)
		}
		if err != nil {
			err = fmt.Errorf("unable to login to target: %s, Error: %s", target, err.Error())
			log.Error(err.Error())
			return err
		}
	}
	return nil
}

// HandleIscsiDiscovery performs iscsi target discovery and create sessions as required.
func HandleIscsiDiscovery(volume *model.Volume) (err error) {
	log.Tracef(">>>>> HandleIscsiDiscovery for volume %s, lun %s", volume.SerialNumber, volume.LunID)
	defer log.Tracef("<<<<< HandleIscsiDiscovery")

	// Do iscsi discovery for Primary Backend

	var primaryVolObj *model.Volume
	primaryVolObj = &model.Volume{}
	primaryVolObj.LunID = volume.LunID
	primaryVolObj.Iqns = volume.Iqns
	primaryVolObj.TargetScope = volume.TargetScope
	primaryVolObj.DiscoveryIPs = volume.DiscoveryIPs
	primaryVolObj.Chap = volume.Chap
	primaryVolObj.ConnectionMode = volume.ConnectionMode
	primaryVolObj.SerialNumber = volume.SerialNumber

	err = handleIscsiDiscoveryForBackend(primaryVolObj, true)

	if err != nil {
		return err
	}
	secondaryBackends := util.GetSecondaryBackends(volume.SecondaryArrayDetails)

	for _, secondaryLunInfo := range secondaryBackends {
		// Do iscsi discovery for Each Secondary Backend
		var secondaryVolObj *model.Volume
		secondaryVolObj = &model.Volume{}
		secondaryVolObj.LunID = strconv.Itoa(int(secondaryLunInfo.LunID))
		secondaryVolObj.Iqns = secondaryLunInfo.TargetNames
		secondaryVolObj.TargetScope = volume.TargetScope
		secondaryVolObj.DiscoveryIPs = secondaryLunInfo.DiscoveryIPs
		secondaryVolObj.Chap = volume.Chap
		secondaryVolObj.ConnectionMode = volume.ConnectionMode
		secondaryVolObj.SerialNumber = volume.SerialNumber

		err = handleIscsiDiscoveryForBackend(secondaryVolObj, true)
	}
	return nil
}
func handleIscsiDiscoveryForBackend(volume *model.Volume, isPrimaryBackend bool) (err error) {
	log.Tracef(">>>>> handleIscsiDiscoveryForBackend for volume obj : %v, isPrimary %v", volume, isPrimaryBackend)
	defer log.Tracef("<<<<< handleIscsiDiscoveryForBackend")
	// determine if all required targets are already logged-in
	loggedIn, err := areTargetsLoggedIn(volume.TargetNames())
	if err != nil {
		return err
	}

	if !loggedIn {
		loginToVolume(volume)
	} else {
		// update chap info for already loggedIn targets
		updateChapForLoggedInTargets(volume)
	}

	// single-target-single-lun models doesn't require SCSI resan to be performed
	if !strings.EqualFold(volume.TargetScope, GroupScope.String()) {
		return nil
	}

	// perform SCSI rescan to create linux block devices
	err = RescanIscsi(volume.LunID)
	if err != nil {
		log.Errorf("Unable to rescan iscsi hosts, Error: %s", err.Error())
		return fmt.Errorf("Unable to rescan iscsi hosts, Error: %s", err.Error())
	}

	return nil
}

func loginToTarget(targets model.IscsiTargets, targetIqn string, ifaces []*model.Iface, chapUser, chapPassword, connectionMode string) (err error) {
	log.Traceln(">>>>> loginToTarget with targetIqn:", targetIqn)
	defer log.Trace("<<<<< loginToTarget")
	for _, target := range targets {
		log.Traceln("Checking with TargetName:", target.Name)
		if target.Name == targetIqn {
			log.Debug("Found target :", "Target:", targetIqn)
			// Now login to the target
			err := addTarget(target, ifaces, chapUser, chapPassword, connectionMode)
			if err != nil {
				if !strings.Contains(err.Error(), alreadyPresent) {
					err = fmt.Errorf("Unable to login to iscsi target %s. Error: %s", target.Name, err.Error())
					log.Error(err.Error())
					// continue with other targets even if there is an error
				}
			}
		}
	}
	return nil
}

func isReachable(initiatorIP, targetIP string) (reachable bool, err error) {
	// Allocate a new ICMP ping object
	pinger, err := ping.NewPinger(targetIP)
	if err != nil {
		log.Errorf("NewPinger creation failure, err=%v", err)
		return false, err
	}

	// Send a "privileged" raw ICMP ping
	pinger.SetPrivileged(true)

	// Ping target from initiatorPort
	if initiatorIP != "" {
		pinger.Source = initiatorIP
	}

	// Count tells pinger to stop after sending (and receiving) Count echo packets
	pinger.Count = PingCount

	// Interval is the wait time between each packet sent
	pinger.Interval = PingInterval

	// Timeout specifies a timeout before ping exits, regardless of how many packets have been received
	pinger.Timeout = PingTimeout

	pinger.OnRecv = func(pkt *ping.Packet) {
		log.Tracef("Received %d bytes from %s: icmp_seq=%d time=%v ttl=%v\n",
			pkt.Nbytes, pkt.Addr, pkt.Seq, pkt.Rtt, pkt.Ttl)
		reachable = true
	}

	// Perform the ping test; if we received any ICMP packet back, stop the test
	log.Tracef("PING %s --> (%s)", initiatorIP, pinger.Addr())
	pinger.Run()

	if !reachable {
		log.Warnf("PING FAILED: %s --> (%s), attempting TCP connection to port 3260", initiatorIP, pinger.Addr())
		// attempt TCP connection to targetip:iscsiport with timeout
		local := &net.TCPAddr{IP: net.ParseIP(initiatorIP)}
		dialer := net.Dialer{Timeout: PingTimeout, LocalAddr: local}

		_, err = dialer.Dial("tcp", fmt.Sprintf("%s:%d", targetIP, DefaultIscsiPort))
		if err != nil {
			log.Warnf("TCP connection attempt failed %s --> (%s:%d), err %s", initiatorIP, targetIP, DefaultIscsiPort, err.Error())
			return false, nil
		}
	}
	return true, nil
}

func updateConnectionMode(target *model.IscsiTarget, connectionMode string) {
	log.Tracef(">>>>> updateConnectionMode for target %s mode %s", target.Name, connectionMode)
	defer log.Trace("<<<<< updateConnectionMode")

	if !(connectionMode == "automatic" || connectionMode == "manual") {
		err := fmt.Errorf("unsupported iscsi connection mode %s", connectionMode)
		log.Debug(err.Error())
		return
	}

	log.Tracef("updating node.startup for target %s address %s", target.Name, target.Address)
	// update connection mode of all targets with same target name
	args := []string{"--mode", "node", "--targetname", target.Name, "--portal", target.Address, "--op", "update", "-n", "node.startup", "-v", connectionMode}
	_, _, err := util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		err = fmt.Errorf("Unable to update node.startup to %s for node %s error %s", connectionMode, target.Name, err.Error())
		log.Errorf(err.Error())
	}
	return
}

// updates iscsi node db with given chap username
func updateChapUser(target *model.IscsiTarget, chapUser string) (err error) {
	log.Tracef(">>>>> updateChapUser for target %s portal %s, user %s", target.Name, target.Address, chapUser)
	defer log.Trace("<<<<< updateChapUser")

	// update nodeChapUser of all targets with same target name
	args := []string{"--mode", "node", "--targetname", target.Name, "--portal", target.Address, "--op", "update", "-n", nodeChapUser, "-v", chapUser}
	_, _, err = util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		log.Errorf("Unable to update chap username for node %s error %s", target.Name, err.Error())
		return fmt.Errorf("Unable to update chap username for node %s error %s", target.Name, err.Error())
	}
	return nil
}

// updates iscsi node db with given chap password
func updateChapPassword(target *model.IscsiTarget, chapPassword string) (err error) {
	log.Tracef(">>>>> updateChapPassword for target %s portal %s", target.Name, target.Address)
	defer log.Trace("<<<<< updateChapPassword")

	// update nodeChapPassword of all targets with same target name
	args := []string{"--mode", "node", "--targetname", target.Name, "--portal", target.Address, "--op", "update", "-n", nodeChapPassword, "-v", chapPassword}
	_, _, err = util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		log.Errorf("Unable to update chap password for node %s address %s error %s", target.Name, target.Address, err.Error())
		return fmt.Errorf("Unable to update chap password for node %s error %s", target.Name, err.Error())
	}
	return nil
}

// addTarget : adds iscsi target to iscsi database
func addTarget(target *model.IscsiTarget, ifaces []*model.Iface, chapUser, chapPassword, connectionMode string) (err error) {
	log.Tracef(">>>>> addTarget called with target: %s address: %s port: %s", target.Name, target.Address, target.Port)
	defer log.Trace("<<<<< addTarget")

	iscsiMutex.Lock()
	defer iscsiMutex.Unlock()

	// update chapuser irrespective (could be a toggle case chap->empty)
	err = updateChapUser(target, chapUser)
	if err != nil {
		log.Errorf(err.Error())
	}
	err = updateChapPassword(target, chapPassword)
	if err != nil {
		log.Errorf(err.Error())
	}

	var out string
	args := []string{"--mode", "node", "--targetname", target.Name, "--portal", target.Address, "--login"}

	if len(ifaces) > 0 {
		for _, iface := range ifaces {
			// verify if the target is reachable from this interface
			reachable, err := isReachable(iface.NetworkInterface.AddressV4, target.Address)
			if err != nil {
				log.Warnf("failed to run ping test from %s --> (%s)", iface.NetworkInterface.AddressV4, target.Address)
				// if we cannot issue ping for some reason, proceed with iscsi login
				reachable = true
			}
			if !reachable {
				log.Errorf("ping test failed from %s --> (%s), skipping iscsi login on this portal to %s", iface.NetworkInterface.AddressV4, target.Address, target.Name)
				continue
			}
			// login using each iface bound
			ifaceArgs := append(args, "-I")
			ifaceArgs = append(ifaceArgs, iface.Name)
			out, _, err = util.ExecCommandOutput(iscsicmd, ifaceArgs)
			// error cases continue to login using other ifaces
			if err != nil {
				log.Debugf("iscsi login failed using iface " + iface.Name + "Error :" + err.Error())
			}
			log.Trace("addTarget Response :", out)
		}
	} else {
		out, _, err = util.ExecCommandOutput(iscsicmd, args)
		if err != nil {
			log.Debugf("iscsi login failed for %s Error %s", target.Name, err.Error())
		}
		log.Trace("addTarget Response :", out)
	}

	// update connection
	if connectionMode != "" {
		updateConnectionMode(target, connectionMode)
	} else {
		log.Tracef("using node.startup=automatic for %s", target.Name)
	}

	if err != nil {
		return fmt.Errorf("Unable to add login to iscsi target %s. Error: %s", target.Name, err.Error())
	}
	return err
}

// GetIscsiIfacesPath returns actual path for iscsi ifaces db
func GetIscsiIfacesPath() (ifacesPath string, err error) {
	log.Trace(">>>>> GetIscsiIfacesPath")
	defer log.Trace("<<<<< GetIscsiIfacesPath")

	fileExists, _, err := util.FileExists(ifacePath)
	if err != nil || !fileExists {
		fileExists, _, err = util.FileExists(altIfacePath)
		if err != nil || !fileExists {
			return "", err
		}
		ifacesPath = altIfacePath
	} else {
		ifacesPath = ifacePath
	}
	log.Trace("ifaces path is ", ifacesPath)
	return ifacesPath, nil
}

// GetIfaces return bound ifaces with network information
func GetIfaces() (ifaces []*model.Iface, err error) {
	log.Trace(">>>>> GetIfaces")
	defer log.Trace("<<<<< GetIfaces")

	ifacesPath, err := GetIscsiIfacesPath()
	if err != nil {
		return nil, fmt.Errorf("Unable to get the iscsi ifaces from the host. Error: %s", err.Error())
	}
	// read iface files
	files, err := ioutil.ReadDir(ifacesPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(files) == 0 {
		log.Trace("no iscsi ifaces found")
		return nil, nil
	}

	// get network interfaces
	networks, err := GetNetworkInterfaces()
	if err != nil {
		return nil, fmt.Errorf("Unable to get the network interfaces on host: Error: %s", err.Error())
	}
	// create ifaces with bound network info
	for _, file := range files {
		lines, _ := util.FileGetStringsWithPattern(fmt.Sprintf("%s/%s", ifacesPath, file.Name()), ifaceNetNamePattern)
		if len(lines) > 0 {
			iface := &model.Iface{Name: file.Name()}
			for _, network := range networks {
				if network.Name == strings.TrimSpace(lines[0]) {
					log.Trace("found iface ", file.Name(), " bound to network ", network)
					iface.NetworkInterface = network
					log.Trace("iface added ", iface.Name)
					ifaces = append(ifaces, iface)
					break
				}
			}
		}
	}
	return ifaces, nil
}

// Check for only supported target types
func isSupportedTarget(targetName string) bool {
	for _, pattern := range targetVendorPatterns {
		if strings.Contains(targetName, pattern) {
			return true
		}
	}
	return false
}

// GetLoggedInIscsiTargets returns currently logged-in iscsi targets
func GetLoggedInIscsiTargets() (targets []string, err error) {
	log.Trace(">>>>> GetLoggedInIscsiTargets")
	defer log.Trace("<<<<< GetLoggedInIscsiTargets")

	// verify iscsi session directory exists
	exists, _, _ := util.FileExists(iscsiSessionDir)
	if !exists {
		return nil, nil
	}
	sessions, err := ioutil.ReadDir(iscsiSessionDir)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch iscsi session entries, err %s", err.Error())
	}
	for _, session := range sessions {
		targetPath := fmt.Sprintf("/sys/class/iscsi_session/%s/targetname", session.Name())
		//TODO : check session state
		exists, _, _ := util.FileExists(targetPath)
		if exists {
			targetName, err := ioutil.ReadFile(targetPath)
			if err != nil {
				// log and continue with other sessions
				log.Warnf("unable to read targetname from %s, err %s", targetPath, err.Error())
				continue
			}
			// ReadFile appends \n to the string
			targetNameStr := strings.TrimSuffix(string(targetName), "\n")
			if isSupportedTarget(targetNameStr) {
				targets = append(targets, targetNameStr)
			}
		}
	}
	if len(targets) > 0 {
		targets = removeDuplicates(targets)
	}
	log.Debugf("found %d logged-in targets", len(targets))
	return targets, nil
}

func removeDuplicates(targets []string) []string {
	// Use map to record duplicates as we find them.
	encountered := map[string]bool{}
	var result []string

	for _, target := range targets {
		if encountered[target] == false {
			// Record this element as an encountered element.
			encountered[target] = true
			// Append to result slice.
			result = append(result, target)
		}
	}
	// Return the new slice.
	return result
}

// GetIscsiTargets gets targets connected on host from /dev/disk/by-path entries
// NOTE: this will fetch only targets with at-least one device discovered
func GetIscsiTargets() (a model.IscsiTargets, err error) {
	log.Trace(">>>>> GetIscsiTargets")
	defer log.Trace("<<<<< GetIscsiTargets")

	var iscsiTargets model.IscsiTargets

	files, err := ioutil.ReadDir(diskbypath)
	if err != nil {
		if os.IsNotExist(err) {
			// /dev/disk/by-path will not be present on some distros if no iscsi sessions are created
			// treat this as no iscsiTargets and not an error condition
			return iscsiTargets, nil
		}
		return iscsiTargets, err
	}

	r := regexp.MustCompile(getByPathTargetPattern())
	for _, f := range files {
		a := r.MatchString(f.Name())
		if a == true {
			iscsiTargetsMatch := r.FindStringSubmatch(f.Name())
			result := make(map[string]string)
			for i, name := range r.SubexpNames() {
				if i != 0 {
					result[name] = iscsiTargetsMatch[i]

				}
			}
			target := &model.IscsiTarget{
				Name:    result["target"],
				Address: result["address"],
				Port:    result["port"],
			}
			iscsiTargets = append(iscsiTargets, target)
		}
	}
	for _, target := range iscsiTargets {
		log.Trace("Target :", target)
	}
	uniqueIscsiTargets := removeDuplicateTargets(iscsiTargets)
	log.Trace("After removing duplicate")
	for _, target := range uniqueIscsiTargets {
		log.Trace("Target :", target)
	}
	return uniqueIscsiTargets, nil
}

// GetChapInfo gets the chap user
func GetChapInfo() (chapInfo *model.ChapInfo, err error) {
	log.Trace(">>>>> GetChapInfo")
	defer log.Trace("<<<<< GetChapInfo")

	chapAuth, err := util.FileGetStringsWithPattern(IscsiConf, chapAuthPattern)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve chap auth method. Error: %s", err.Error())
	}

	if len(chapAuth) == 0 || strings.ToLower(chapAuth[0]) != "chap" {
		log.Trace("chap auth not set")
		return nil, nil
	}

	user, err := util.FileGetStringsWithPattern(IscsiConf, chapUserPattern)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve chap user name. Error: %s", err.Error())
	}
	log.Trace(user)

	password, err := util.FileGetStringsWithPattern(IscsiConf, chapPasswordPattern)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve chap password. Error: %s", err.Error())
	}
	if len(user) > 0 && len(password) > 0 {
		chapInfo, err := validateChapUserPassword(user, password)
		if err != nil {
			return nil, fmt.Errorf("Unable to validate chap user and password. Error: %s", err.Error())
		}
		return chapInfo, nil
	}
	return nil, nil
}

func validateChapUserPassword(user []string, password []string) (chapInfo *model.ChapInfo, err error) {
	log.Trace(">>>>> validateChapUserPassword")
	defer log.Trace("<<<<< validateChapUserPassword")

	if len(password[0]) < chapPasswordMinLen || len(password[0]) > chapPasswordMaxLen {
		return nil, fmt.Errorf("invalid chap password. Should be between %s and %s chars ", strconv.Itoa(chapPasswordMinLen), strconv.Itoa(chapPasswordMaxLen))

	}
	for _, char := range password[0] {
		if !strings.Contains(validChapPasswordChars, string(char)) {
			return nil, fmt.Errorf("invalid chap password. only %s are allowed", validChapPasswordChars)
		}
	}
	chapInfo = &model.ChapInfo{Name: user[0], Password: password[0]}
	return chapInfo, nil
}

// GetIscsiNodesFromIsciadm retrieves iscsi targets with iscsiadm command
func GetIscsiNodesFromIsciadm() (a model.IscsiTargets, err error) {
	log.Tracef(">>>>> GetIscsiNodesFromIsciadm called")
	defer log.Trace("<<<<< GetIscsiNodesFromIsciadm")
	var out string
	var outList []string
	args := []string{"-m", "node"}
	out, _, _ = util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		log.Error(err.Error())
	}
	outList = append(outList, out)
	log.Trace(outList)
	var iscsiTargets model.IscsiTargets
	// parse the lines into output
	r := regexp.MustCompile(getIscsiadmPattern())

	for _, outItem := range outList {
		listOut := r.FindAllString(outItem, -1)
		for _, line := range listOut {
			result := util.FindStringSubmatchMap(line, r)
			target := &model.IscsiTarget{
				Name:    result["target"],
				Address: result["address"],
				Port:    result["port"],
			}
			log.Tracef("Name %s Address %s Port %s", target.Name, target.Address, target.Port)
			iscsiTargets = append(iscsiTargets, target)
		}
	}
	uniqueIscsiTargets := removeDuplicateTargets(iscsiTargets)
	return uniqueIscsiTargets, err
}

// PerformDiscovery : adds iscsi targets to iscsi database after performing
// send targets
func PerformDiscovery(discoveryIPs []string) (a model.IscsiTargets, err error) {
	log.Tracef(">>>>> PerformDiscovery with discovery IPs %s", discoveryIPs)
	defer log.Trace("<<<<< PerformDiscovery")

	iscsiMutex.Lock()
	defer iscsiMutex.Unlock()

	var isDiscoveryIpReachable bool
	var out string
	var outList []string
	for _, discoveryIP := range discoveryIPs {
		// find the first discovery ip which is reachable
		isDiscoveryIpReachable, err = isReachable("", discoveryIP)
		if err != nil {
			continue
		}
		if isDiscoveryIpReachable {
			args := []string{"-m", "discovery", "-t", "st", "-p", discoveryIP, "-o", "new"}
			out, _, err = util.ExecCommandOutput(iscsicmd, args)
			if err != nil {
				log.Error(err.Error())
				continue
			}
			outList = append(outList, out)
		}
	}

	if !isDiscoveryIpReachable {
		return nil, fmt.Errorf("no reachable discovery ip found. Please sanity check host OS and array IP configuration, network, netmask and gateway.")
	}

	if err != nil {
		return nil, err
	}

	log.Trace(outList)

	var iscsiTargets model.IscsiTargets
	// parse the lines into output
	r := regexp.MustCompile(getIscsiadmPattern())

	for _, outItem := range outList {
		listOut := r.FindAllString(outItem, -1)
		for _, line := range listOut {
			result := util.FindStringSubmatchMap(line, r)
			target := &model.IscsiTarget{
				Name:    result["target"],
				Address: result["address"],
				Port:    result["port"],
			}
			log.Tracef("Name %s Address %s Port %s", target.Name, target.Address, target.Port)
			iscsiTargets = append(iscsiTargets, target)
		}
	}

	uniqueIscsiTargets := removeDuplicateTargets(iscsiTargets)

	return uniqueIscsiTargets, err
}

func removeDuplicateTargets(targets model.IscsiTargets) model.IscsiTargets {
	// Use map to record duplicates as we find them.
	encountered := map[string]bool{}
	var result model.IscsiTargets

	for v := range targets {
		if encountered[targets[v].Name+targets[v].Address+targets[v].Port] == false {
			// Record this element as an encountered element.
			encountered[targets[v].Name+targets[v].Address+targets[v].Port] = true
			// Append to result slice.
			result = append(result, targets[v])
		}
	}
	// Return the new slice.
	return result
}

// iscsiGetTargetsOfDevice : get the iscsi target information of a device from sysfs
func iscsiGetTargetsOfDevice(dev *model.Device) (target []*model.IscsiTarget, err error) {
	log.Trace(">>>>> iscsiGetTargetsOfDevice  with", dev)
	defer log.Trace("<<<<< iscsiGetTargetsOfDevice")

	r := regexp.MustCompile(sessionIDPattern)
	iscsiTargets := make([]*model.IscsiTarget, 0)
	for _, hcil := range dev.Hcils {
		host := strings.Split(hcil, ":")[0]
		iscsiHostPath := fmt.Sprintf(hostDeviceFormat, host)
		log.Trace(iscsiHostPath)
		args := []string{iscsiHostPath}
		out, _, err := util.ExecCommandOutput(lscmd, args)
		if err != nil {
			if os.IsNotExist(err) {
				// Not an iSCSI device
				return nil, nil
			}
			return nil, err
		}
		if r.MatchString(out) {
			lsOutMatch := r.FindStringSubmatch(out)
			log.Trace(lsOutMatch)
			if len(lsOutMatch) < 2 {
				return nil, fmt.Errorf("no iscsi session match found")
			}
			sessionID := lsOutMatch[1]
			log.Trace(sessionID)
			iscsiTarget, err := getIscsiTargetFromSessionID(dev, host, sessionID)
			if err != nil {
				return nil, fmt.Errorf("no iscsi target found from iscsi session %s", sessionID)
			}
			iscsiTargets = append(iscsiTargets, iscsiTarget)
		}
	}
	return iscsiTargets, nil
	
	
}

func getIscsiTargetFromSessionID(dev *model.Device, host string, sessionID string) (*model.IscsiTarget, error) {
	log.Tracef(">>> getIscsiTargetFromSessionID with device %s", dev.Pathname)
	defer log.Trace("<<< getIscsiTargetFromSessionID")

	hostTargetPath := fmt.Sprintf(hostTargetNameFormat, host, sessionID, sessionID)
	targetName, err := util.FileReadFirstLine(hostTargetPath)
	if err != nil {
		return nil, err
	}
	hostTagPath := fmt.Sprintf(hostTagNameFormat, host, sessionID, sessionID)
	tag, err := util.FileReadFirstLine(hostTagPath)
	// ignore errors when session is not connected. We should stil return basic target with iqn
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), endPointNotConnected) {
		return nil, err
	}

	hostTargetAddressPath := fmt.Sprintf(hostTargetAddressFormat, host, sessionID, sessionID, sessionID)
	address, err := util.FileReadFirstLine(hostTargetAddressPath)
	// ignore errors when session is not connected. We should stil return basic target with iqn
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), endPointNotConnected) {
		return nil, err
	}

	hostTargetPortPath := fmt.Sprintf(hostTargetPortFormat, host, sessionID, sessionID, sessionID)
	port, err := util.FileReadFirstLine(hostTargetPortPath)
	// ignore errors when session is not connected. We should stil return basic target with iqn
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), endPointNotConnected) {
		return nil, err
	}

	log.Debugf("iscsi target details obtained for device %s, address %s, targetName %s, port %s", dev.Pathname, address, targetName, port)

	iscsiTarget := &model.IscsiTarget{
		Name:    targetName,
		Address: address,
		Port:    port,
		Tag:     tag,
	}
	return iscsiTarget, nil
}

// iscsiLogoutOfTarget : logout the iscsi target
func iscsiLogoutOfTarget(target *model.IscsiTarget) (err error) {
	log.Trace(">>>>> iscsiLogoutOfTarget with", target)
	defer log.Trace("<<<<< iscsiLogoutOfTarget")
	iscsiMutex.Lock()
	defer iscsiMutex.Unlock()

	if target == nil || target.Name == "" {
		return fmt.Errorf("Empty target name provided to logout")
	}
	args := []string{"--mode", "node", "-u", "-T", target.Name}
	out, _, err := util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		return fmt.Errorf("logout failed for %s. Error : %s", target.Name, err.Error())
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, successfulDot) == true {
			return nil
		}
	}
	return fmt.Errorf("logout failed for target %s. Error :%s", target.Name, out)
}

// iscsiDeleteNode : delete the iscsi node from iscsi database
func iscsiDeleteNode(target *model.IscsiTarget) (err error) {
	log.Trace(">>>>> iscsiDeleteNode called with", target)
	defer log.Trace("<<<<< iscsiDeleteNode")
	iscsiMutex.Lock()
	defer iscsiMutex.Unlock()

	if target == nil || target.Name == "" {
		return fmt.Errorf("Empty target to delete Node")
	}
	args := []string{"--mode", "node", "-o", "delete", "-T", target.Name}
	_, _, err = util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		return fmt.Errorf("delete failed for %s. Error %s", target.Name, err.Error())
	}

	return nil
}

func getIscsiHosts() ([]string, error) {
	exists, _, err := util.FileExists(iscsiHostPathFormat)
	if !exists {
		log.Errorf("no iscsi hosts found")
		return nil, fmt.Errorf("no iscsi hosts found")
	}

	listOfFiles, err := ioutil.ReadDir(iscsiHostPathFormat)
	if err != nil {
		log.Errorf("unable to get list of iscsi hosts, error %s", err.Error())
		return nil, fmt.Errorf("unable to get list of iscsi hosts, error %s", err.Error())
	}

	if len(listOfFiles) == 0 {
		return nil, nil
	}
	var iscsiHosts []string
	for _, file := range listOfFiles {
		log.Tracef("host name %s", file.Name())
		iscsiHosts = append(iscsiHosts, file.Name())
	}

	log.Tracef("iscsiHosts %v", iscsiHosts)

	return iscsiHosts, nil
}

// RescanIscsi perform SCSI rescan on iSCSI host ports
func RescanIscsi(lunID string) error {
	log.Tracef(">>> RescanIscsi initiated for lunID %s", lunID)
	defer log.Traceln("<<< RescanIscsi")
	iscsiHosts, err := getIscsiHosts()
	if err != nil {
		return err
	}
	err = rescanIscsiHosts(iscsiHosts, lunID)
	if err != nil {
		return err
	}
	return nil
}

// nolint: dupl
func rescanIscsiHosts(iscsiHosts []string, lunID string) (err error) {
	for _, iscsiHost := range iscsiHosts {
		// perform rescan for all hosts
		if iscsiHost != "" {
			log.Tracef("rescanHost initiated for %s", iscsiHost)
			iscsiHostScanPath := fmt.Sprintf(iscsiHostScanPathFormat, iscsiHost)
			isIscsiHostScanPathExists, _, _ := util.FileExists(iscsiHostScanPath)
			if !isIscsiHostScanPathExists {
				log.Tracef("iscsi host scan path %s does not exist", iscsiHostScanPath)
				continue
			}

			if lunID == "" {
				err = ioutil.WriteFile(iscsiHostScanPath, []byte("- - -"), 0644)
				if err != nil {
					log.Tracef("error writing to file %s : %s", iscsiHostScanPath, err.Error())
				}
			} else {
				log.Printf("\n SCANNING iscsi lun id %v", lunID)
				err = ioutil.WriteFile(iscsiHostScanPath, []byte("- - "+lunID), 0644)
				if err != nil {
					log.Tracef("error writing to file %s : %s", iscsiHostScanPath, err.Error())
				}
			}
			if err != nil {
				log.Errorf("unable to rescan for scsi devices on host %s err %s", iscsiHost, err.Error())
				return fmt.Errorf("unable to rescan for scsi devices on host %s err %s", iscsiHost, err.Error())
			}
		}
	}
	return nil
}

func addIscsiPortBinding(networks []*model.NetworkInterface) error {
	// Get iscsi ifaces bound to network interfaces
	ifaces, err := GetIfaces()
	// treat iface path not found error as no ifaces bound
	if err != nil && !os.IsNotExist(err) {
		log.Errorf("Unable to retrieve iSCSI bound ifaces. Error: %s", err.Error())
		return fmt.Errorf("Unable to retrieve iSCSI bound ifaces. Error: %s", err.Error())
	}
	// create iface for each network specified, if there is one not created
	for _, network := range networks {
		found := false
		bound := false
		iface := getMatchingIface(ifaces, network)
		if iface != nil {
			found = true
			// check if iface had binding with network interface
			if iface.NetworkInterface.Name == network.Name {
				bound = true
			}
		}
		if !found {
			err = createIface(*network)
			if err != nil {
				return err
			}
		}
		if !bound {
			err = bindIface(*network)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// returns iface matching the network address specified or nil otherwise
func getMatchingIface(ifaces []*model.Iface, network *model.NetworkInterface) (iface *model.Iface) {
	for _, iface := range ifaces {
		if network.AddressV4 == iface.NetworkInterface.AddressV4 {
			return iface
		}
	}
	return nil
}

// creates an iSCSI iface for specified network name
func createIface(network model.NetworkInterface) error {
	iscsiMutex.Lock()
	defer iscsiMutex.Unlock()

	// iscsiadm -m iface -I iface_eth2 --op=new
	args := []string{"-m", "iface", "-I", fmt.Sprintf("iface_%s", network.Name), "--op", "new"}
	_, _, err := util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		return err
	}
	return nil
}

// binds an iSCSI iface with specified network name
func bindIface(network model.NetworkInterface) error {
	iscsiMutex.Lock()
	defer iscsiMutex.Unlock()

	// iscsiadm -m iface -I iface_eth2 --op=update -n iface.net_ifacename -v eth2
	args := []string{"-m", "iface", "-I", fmt.Sprintf("iface_%s", network.Name), "--op=update", "-n", "iface.net_ifacename", "-v", network.Name}
	_, _, err := util.ExecCommandOutput(iscsicmd, args)
	if err != nil {
		return err
	}
	return nil
}
