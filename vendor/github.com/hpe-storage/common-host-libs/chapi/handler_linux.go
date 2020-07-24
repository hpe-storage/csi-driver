package chapi

// Copyright 2019 Hewlett Packard Enterprise Development LP
import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/mux"

	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/tunelinux"
	"github.com/hpe-storage/common-host-libs/util"
)

var (
	createDeviceLock sync.Mutex
	removeDeviceLock sync.Mutex
	//ChapidSocketPath : the directory for chapid socket
	ChapidSocketPath = "/opt/hpe-storage/etc/"
	//ChapidSocketName : chapid socket name
	ChapidSocketName = "chapid"
	driver           Driver
)

// initialize the host gob file only once
func init() {
	log.Trace("Initializing the CHAPI linux driver")
	driver = &LinuxDriver{}
}

//@APIVersion 1.0.0
//@Title getHosts
//@Description retrieves hosts
//@Accept json
//@Resource /hosts
//@Success 200 {array} Hosts
//@Router /hosts/ [get]
func getHosts(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response

	hosts, err := driver.GetHosts()
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	chapiResp.Data = hosts
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getHostInfo
//@Description retrieves hosts
//@Accept json
//@Resource /hosts/
//@Success 200 Host
//@Router /hosts/{id} [get]
func getHostInfo(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]

	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	host, err := driver.GetHostInfo()
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	host.UUID = id

	chapiResp.Data = host
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getHostNameAndDomain
//@Description retrieves host domain and name
//@Accept json
//@Resource /hosts/
//@Success 200 Host
//@Router /hosts/{id}/hostname [get]
func getHostNameAndDomain(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]

	log.Info(">>>>> getHostNameAndDomain called for host with id ", id)
	defer log.Info("<<<<< getHostNameAndDomain")

	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	hostanddomain, err := driver.GetHostNameAndDomain()
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	host := &model.Host{Name: hostanddomain[0], Domain: hostanddomain[1]}
	chapiResp.Data = host
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getChapInfo
//@Description get chapInfo
//@Accept json
//@Resource /getChapInfo
//@Success 200 model.ChapInfo
//@Router /hosts/{hostid}/getchapinfo [get]
func getChapInfo(w http.ResponseWriter, r *http.Request) {
	function := func() (interface{}, error) {
		return linux.GetChapInfo()
	}
	handleRequest(function, "getChapInfo", w, r)
}

//@APIVersion 1.0.0
//@Title getDevices
//@Description retrieves devices for the Host with id=id
//@Accept json
//@Resource /devices
//@Success 200 {array} model.Devices
//@Router /hosts/{id}/devices [get]
func getDevices(w http.ResponseWriter, r *http.Request) {
	function := func() (interface{}, error) {

		return linux.GetLinuxDmDevices(false, util.GetVolumeObject("", ""))
	}
	handleRequest(function, "getDevices", w, r)
}

// Create linux device with attributes passed in the body of the http request
//@APIVersion 1.0.0
//@Title createDevices
//@Description create linux devices for the VolumeInfos passed
//@Accept json
//@Resource /devices
//@Success 200 {array} model.Devices
//@Router /hosts/{id}/devices/
func createDevices(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]

	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	createDeviceLock.Lock()
	defer createDeviceLock.Unlock()
	var vols []*model.Volume
	decoder := json.NewDecoder(r.Body)
	err = decoder.Decode(&vols)
	defer r.Body.Close()

	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	devices, err := driver.CreateDevices(vols)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	chapiResp.Data = devices
	json.NewEncoder(w).Encode(chapiResp)
}

// offline : offline the device from the host
//@APIVersion 1.0.0
//@Title offlineDevice
//@Description offline device for host id=id and device serialnumber=serialnumber
//@Accept json
//@Resource /devices
//@Success 200
//@Router /hosts/{id}/devices/{serialnumber}/actions/offline [put]
func offlineDevice(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]
	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	var device *model.Device
	decoder := json.NewDecoder(r.Body)
	err = decoder.Decode(&device)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	err = driver.OfflineDevice(device)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	chapiResp.Data = &model.Device{}
	json.NewEncoder(w).Encode(chapiResp)
	return
}

// deleteDevice : disconnect and delete the device from the host
//@APIVersion 1.0.0
//@Title deleteDevice
//@Description delete device for host id=id and device serialnumber=serialnumber
//@Accept json
//@Resource /devices
//@Success 200
//@Router /hosts/{id}/devices/{serialnumber} [delete]
func deleteDevice(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]
	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	removeDeviceLock.Lock()
	defer removeDeviceLock.Unlock()

	var device *model.Device
	decoder := json.NewDecoder(r.Body)
	err = decoder.Decode(&device)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	err = driver.DeleteDevice(device)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	chapiResp.Data = &model.Device{}
	json.NewEncoder(w).Encode(chapiResp)
}

// GetDeviceForSerialNumber get the device for that serialnumber
//@APIVersion 1.0.0
//@Title getDeviceForSerialNumber
//@Description get linux device for host id=id and device serialnumber=serialnumber
//@Accept json
//@Resource /devices
//@Success 200 {array} model.Device
//@Router /hosts/{id}/devices/{serialnumber} [get]
func getDeviceForSerialNumber(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]
	serialnumber := vars["serialnumber"]

	err := validateHostIDAndSerialNumber(id, serialnumber)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	devices, err := linux.GetLinuxDmDevices(false, util.GetVolumeObject(serialnumber, ""))
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	for _, d := range devices {
		if d.SerialNumber == serialnumber {
			log.Tracef("Found serial number from list of devices: %s", serialnumber)
			chapiResp.Data = d
			json.NewEncoder(w).Encode(chapiResp)
			return
		}
	}

	log.Traceln("Device with serialnumber", serialnumber, "not found")
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getPartitionsForDevice
//@Description get all partitions for a linux device for host id=id and device serialnumber=serialnumber
//@Accept json
//@Resource /partitions
//@Success 200 {array} model.DevicePartitions
//@Router /hosts/{id}/devices/{serialnumber}/partitions [get]
func getPartitionsForDevice(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]
	serialnumber := vars["serialnumber"]

	err := validateHostIDAndSerialNumber(id, serialnumber)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	devices, err := linux.GetLinuxDmDevices(false, util.GetVolumeObject(serialnumber, ""))
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	for _, d := range devices {
		if d.SerialNumber == serialnumber {
			log.Tracef("Found serial number from list of devices: %s", serialnumber)
			// Located the device. Now find all partitions
			partitions, err := linux.GetPartitionInfo(d)
			if err != nil {
				handleError(w, chapiResp, err, http.StatusInternalServerError)
				return
			}
			chapiResp.Data = partitions
			json.NewEncoder(w).Encode(chapiResp)
			return
		}
	}
}

//@APIVersion 1.0.0
//@Title getMountsOnHostForSerialNumber
//@Description get all mounts for devices for host id=id
//@Accept json
//@Resource /mounts
//@Success 200 {array} model.Mounts
//@Router /hosts/{id}/mounts/{serialNumber} [get]
func getMountsOnHostForSerialNumber(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]
	serialNumber := vars["serialNumber"]

	log.Tracef(">>>>> getMountsOnHostForSerialNumber with serial number %s", serialNumber)
	defer log.Tracef("<<<<< getMountsOnHostForSerialNumber")

	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	mounts, err := driver.GetMounts(serialNumber)
	for _, mount := range mounts {
		log.Tracef("retrieved mount %#v for serialNumber %s", mount, serialNumber)
	}
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	chapiResp.Data = mounts
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getMountForDevice
//@Description get mountpoint for device for host id=id and serialnumber=serialnumber
//@Accept json
//@Resource /mounts
//@Success 200 model.Mount
//@Router /hosts/{id}/mounts/{mountid}/{serialNumber} [get]
func getMountForDevice(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	hostid := vars["id"]

	err := validateHostIDAndMountID(hostid, vars["mountid"])
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	//mountid, _ := strconv.ParseUint(vars["mountid"], 10, 64)
	mountid := vars["mountid"]
	serialNumber := vars["serialNumber"]

	devices, err := linux.GetLinuxDmDevices(false, util.GetVolumeObject(serialNumber, ""))
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	mounts, err := linux.GetMountPointsForDevices(devices)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	for _, m := range mounts {
		if m.ID == mountid {
			chapiResp.Data = m
			json.NewEncoder(w).Encode(chapiResp)
			return
		}
	}
}

//@APIVersion 1.0.0
//@Title CreateFileSystem on device
//@Description create a filesysten for a linux device for host id=id and device serialnumber=serialnumber
//@Accept json
//@Resource /devices/{serialnumber}/{filesystem}
//@Success 200 {array} linux.CreateFileSystem
//@Router /hosts/{id}/devices/{serialnumber}/{filesystem} [put]
func createFileSystemOnDevice(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]
	serialnumber := vars["serialnumber"]
	filesystem := vars["filesystem"]

	err := validateHostIDAndSerialNumber(id, serialnumber)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}
	if filesystem == "" {
		filesystem = "xfs"
	}
	log.Trace("creating filesystem", filesystem)
	device, err := linux.CreateFileSystemOnDevice(serialnumber, filesystem)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	chapiResp.Data = device
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title  Mount  device to the host
//@Description Mount an attached linux device with a File System passed in the body on the host
//@Accept json
//@Resource /mounts
//@Success 200 {array} model.Mount
//@Router /hosts/{id}/mounts [post]
func mountDevice(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	var mount *model.Mount
	var device *model.Device
	id := vars["id"]
	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(buf, &mount)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	device = mount.Device
	mountPoint := mount.Mountpoint

	mnt, err := driver.MountDevice(device, mountPoint, nil, nil)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	chapiResp.Data = mnt

	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title  Unmount  device from the host
//@Description Unmount an attached linux device with a File System from the host
//@Accept json
//@Resource /mounts
//@Success 200 {array} model.Mount
//@Router /hosts/{id}/mounts/{mountID} [delete]
func unmountDevice(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]
	mountID := vars["mountID"]

	err := validateHostIDAndMountID(id, mountID)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	buf, err := ioutil.ReadAll(r.Body)
	var mount *model.Mount
	var device *model.Device
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(buf, &mount)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	if mount.Device == nil || mount.Mountpoint == "" {
		err = errors.New("No device or mount point found")
		chapiResp.Err = ErrorResponse{Info: err.Error()}
		json.NewEncoder(w).Encode(chapiResp)
		return

	}
	device = mount.Device
	mountPoint := mount.Mountpoint

	mnt, err := driver.UnmountDevice(device, mountPoint)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	chapiResp.Data = mnt
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getHostInitiators
//@Description get Initiators for=host id=id
//@Accept json
//@Resource /initiators
//@Success 200 model.Initiators
//@Router /hosts/{hostid}/initiators [get]
func getHostInitiators(w http.ResponseWriter, r *http.Request) {
	function := func() (interface{}, error) {
		return driver.GetHostInitiators()
	}
	handleRequest(function, "getHostInitiators", w, r)
}

//@APIVersion 1.0.0
//@Title getHostNetworks
//@Description get Initiators for=host id=id
//@Accept json
//@Resource /network
//@Success 200 model.Network
//@Router /hosts/{id}/network [get]
func getHostNetworks(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	var nics []*model.NetworkInterface
	vars := mux.Vars(r)
	hostid := vars["id"]

	err := validateHost(hostid)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	nics, err = linux.GetNetworkInterfaces()
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	if len(nics) == 0 {
		log.Error("No Network found on host")
		chapiResp.Err = ErrorResponse{Info: errors.New("No Network found on the host").Error()}
		json.NewEncoder(w).Encode(chapiResp)
	}
	chapiResp.Data = nics
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getDeletingDevices
//@Description get devices in deletion state for=host id=id
//@Accept json
//@Resource /deletingdevices
//@Success 200 linux.Recommendation
//@Router /hosts/{id}/deletingdevices [get]
func getDeletingDevices(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	vars := mux.Vars(r)
	hostid := vars["id"]

	err := validateHost(hostid)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}
	devices := linux.GetDeletingDevices()
	if devices == nil {
		// this is not an error condition, but just means no deletions are pending
		log.Trace("no device deletions pending")
	}

	chapiResp.Data = devices
	json.NewEncoder(w).Encode(chapiResp)
}

//@APIVersion 1.0.0
//@Title getHostRecommendations
//@Description get Recommendations for=host id=id
//@Accept json
//@Resource /recommendations
//@Success 200 linux.Recommendation
//@Router /hosts/{id}/recommendations [get]
func getHostRecommendations(w http.ResponseWriter, r *http.Request) {
	var chapiResp Response
	var settings []*tunelinux.Recommendation
	vars := mux.Vars(r)
	hostid := vars["id"]

	err := validateHost(hostid)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	settings, err = tunelinux.GetRecommendations()
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}
	chapiResp.Data = settings
	json.NewEncoder(w).Encode(chapiResp)
}

func validateHost(hostID string) error {
	log.Trace(">>>>> validateHost")
	defer log.Trace("<<<<< validateHost")

	if hostID == "" {
		log.Error("Empty id in the request")
		return errors.New("Empty id in the request")
	}

	host, err := getHostFromUUID(hostID)
	if err != nil {
		log.Error("Unable to retrieve host from", "id", hostID)
		return errors.New("Unable to retrieve host from id " + hostID)
	}

	log.Tracef("host=%s retrieved from id=:%s", host.Name, hostID)
	return nil
}

func validateHostIDAndMountID(hostID, mountID string) error {
	log.Trace(">>>>> validateHostIDAndMountID")
	defer log.Trace("<<<<< validateHostIDAndMountId")

	err := validateHost(hostID)
	if err != nil {
		return err
	}
	if mountID == "" {
		return fmt.Errorf("empty mountID in the request")
	}
	_, err = strconv.ParseUint(mountID, 10, 64)
	if err != nil {
		return fmt.Errorf("Unable to parse MountID in the request")
	}

	return nil
}

func validateHostIDAndSerialNumber(hostID, serialNumber string) error {
	log.Trace(">>>>> validateHostIDAndSerialNumber")
	defer log.Trace("<<<<< validateHostIDAndSerialNumber")

	err := validateHost(hostID)
	if err != nil {
		return err
	}
	if serialNumber == "" {
		return errors.New("Empty SerialNumber in the request")
	}

	return nil
}

// GetHostFromUUID retrieves Host for that uuid
func getHostFromUUID(id string) (*model.Host, error) {
	hosts, err := driver.GetHosts()
	if err != nil {
		return nil, err
	}
	for _, host := range *hosts {
		if host.UUID == id {
			// Host Matches
			log.Tracef("current host matches with id=%s", id)
			return host, nil
		}
	}
	return nil, fmt.Errorf("no host found with id %s", id)
}

// standard method for handling requests
func handleRequest(function func() (interface{}, error), functionName string, w http.ResponseWriter, r *http.Request) {
	log.Info(">>>>> " + functionName)
	defer log.Info("<<<<< " + functionName)

	var chapiResp Response
	vars := mux.Vars(r)
	id := vars["id"]

	err := validateHost(id)
	if err != nil {
		handleError(w, chapiResp, err, http.StatusBadRequest)
		return
	}

	data, err := function()
	if err != nil {
		handleError(w, chapiResp, err, http.StatusInternalServerError)
		return
	}

	chapiResp.Data = data
	json.NewEncoder(w).Encode(chapiResp)
}

func handleError(w http.ResponseWriter, chapiResp Response, err error, statusCode int) {
	log.Trace("Err :", err)
	w.WriteHeader(statusCode)
	chapiResp.Err = ErrorResponse{Info: err.Error()}
	json.NewEncoder(w).Encode(chapiResp)
}
