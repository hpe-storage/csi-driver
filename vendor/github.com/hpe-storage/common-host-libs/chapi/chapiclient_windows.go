// (c) Copyright 2018 Hewlett Packard Enterprise Development LP

package chapi

import (
	"errors"
	"fmt"
	"github.com/hpe-storage/common-host-libs/connectivity"
	"github.com/hpe-storage/common-host-libs/jsonutil"
	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
	"io/ioutil"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	chapiPortFilePath = "c:\\ProgramData\\Nimble Storage\\CHAPI\\CHAPIPort.txt"
)

func init() {
	// To maintain backward compatibility, update the chapiPortFilePath only if CHAPI is installed  by Windows Docker Volume plugin

	WinChapiPortFilePath := "c:\\Program Files\\hpe-storage\\chapi\\CHAPIPort.txt"
	if _, err := os.Stat(WinChapiPortFilePath); err == nil {
		chapiPortFilePath = WinChapiPortFilePath

	}
}

//MountResponse : mount response for docker
type MountResponse struct {
	MountPoint string `json:"Mountpoint,omitempty"`
	Err        string `json:"Err"`
}

// GetSocketName returns unix socket name (per process)
func GetSocketName() string {
	log.Errorln("GetSocketName: Not supported on", runtime.GOOS)
	return ""
}

// NewChapiClientWithTimeout returns the chapi client with timeout configuration that communicates over HTTP
func NewChapiClientWithTimeout(timeout time.Duration) (*Client, error) {
	var chapiClient *Client
	var err error

	log.Traceln("CHAPI port filepath:", chapiPortFilePath)
	// Read CHAPI Port from local file (Windows)
	buf, err := ioutil.ReadFile(chapiPortFilePath)
	if err != nil {
		log.Traceln("Failed to read CHAPI port file:", chapiPortFilePath)
		return nil, err
	}
	chapiPort := string(buf)
	chapiPort = strings.TrimSuffix(chapiPort, "\r\n")
	log.Tracef("CHAPI port obtained is '%v' from file '%v'", chapiPort, chapiPortFilePath)
	hostName := "http://127.0.0.1"
	port, err := strconv.ParseUint(chapiPort, 10, 64)
	if err != nil {
		return nil, err
	}
	chapiClient, err = NewChapiHTTPClientWithTimeout(hostName, port, timeout)
	if err != nil {
		return nil, err
	}

	// Obtain access/secret key and insert as HTTP headers
	var accessKey string
	accessKey, err = chapiClient.GetAccessKey()
	if err != nil {
		log.Errorln("Failed to get host access-key")
		return nil, err
	}
	header := map[string]string{"CHAPILocalAccessKey": accessKey}
	// Insert HTTP header
	chapiClient.AddHeader(header)
	return chapiClient, nil
}

// GetAccessKey will retrieve the access key from the host.
func (chapiClient *Client) GetAccessKey() (string, error) {
	log.Trace("GetAccessKey called")
	var accessKeyPath AccessKeyPath
	var errResp *ErrorResponse
	var chapiResp Response
	chapiResp.Data = &accessKeyPath
	chapiResp.Err = &errResp
	_, err := chapiClient.client.DoJSON(&connectivity.Request{Action: "GET", Path: "/keyfile", Payload: nil, Response: &chapiResp, ResponseError: &chapiResp})
	if errResp != nil {
		log.Trace(errResp.Info)
		return "", errors.New(errResp.Info)
	}
	if err != nil {
		log.Trace("Err :", err.Error())
		return "", err
	}
	// Dump access key file path
	jsonutil.PrintPrettyJSONToLog(accessKeyPath)
	// Read the file and extract accessKey
	buf, err := ioutil.ReadFile(accessKeyPath.Path)
	if err != nil {
		return "", err
	}
	accessKey := string(buf) //"577c9b13-0604-478e-9708-a49ab3139b8a"
	accessKey = strings.TrimSuffix(accessKey, "\r\n")
	log.Traceln("host access-key obtained is :", accessKey)

	return accessKey, nil
}

// NewChapiClient returns the chapi client that communicates over the required unix socket(per process or common service)
func NewChapiClient() (*Client, error) {
	var chapiClient *Client
	var err error

	//windows port is required
	//chapiClient, err = NewChapiHTTPClient("http://127.0.0.1", 49725)
	var timeout time.Duration = 5 * time.Minute
	chapiClient, err = NewChapiClientWithTimeout(timeout)
	if err != nil {
		log.Errorln(err.Error())
		return nil, err
	}

	return chapiClient, nil
}

// GetHostName returns hostname and domain name of given host
func (chapiClient *Client) GetHostName() (host *model.Host, err error) {
	log.Trace("GetHostName called")

	// fetch host ID
	err = chapiClient.cacheHostID()
	if err != nil {
		return nil, err
	}
	var hosts []*model.Host
	var hostnameURI string

	var chapiResp Response
	var errResp *ErrorResponse
	chapiResp.Err = &errResp

	chapiResp.Data = &hosts
	hostnameURI = "/hosts"

	log.Trace("GetHostName called with URI %s", hostnameURI)
	_, err = chapiClient.client.DoJSON(&connectivity.Request{Action: "GET", Path: hostnameURI, Header: chapiClient.header, Payload: nil, Response: &chapiResp, ResponseError: &chapiResp})
	if err != nil {
		if errResp != nil {
			log.Error(errResp.Info)
			return nil, errors.New(errResp.Info)
		}
		log.Trace("Err :", err.Error())
		return nil, err
	}

	return hosts[0], nil
}

// SetupFilesystemAndPermissions creates filesystem and applies permissions if present
func (chapiClient *Client) SetupFilesystemAndPermissions(device *model.Device, vol *model.Volume, filesystem string) error {
	log.Tracef("SetupFilesystemAndPermissions called for volume (%s), mountpoint(%s)", vol.Name, vol.MountPoint)
	err := chapiClient.CreateFilesystem(device, vol, filesystem)
	if err != nil {
		return fmt.Errorf("unable to create filesystem %s", err.Error())
	}

	var getRespMount []*model.Mount

	err = chapiClient.GetMounts(&getRespMount, device.SerialNumber)
	if err != nil && !(strings.Contains(err.Error(), "object was not found")) {
		return err
	}
	mountID := getRespMount[0].ID

	var reqMount, respMount *model.Mount
	log.Trace("MountID :", mountID)
	reqMount = &model.Mount{
		Mountpoint: vol.MountPoint,
		Device:     device,
		ID:         mountID,
	}

	err = chapiClient.Mount(reqMount, respMount)
	if err != nil {
		log.Trace("Err :", err.Error())
		return err
	}

	return nil
}

// MatchMountId calls return the matching mountId
func MatchMountId(getRespMount []*model.Mount, device *model.Device) string {
	var mountId string
	for _, mount := range getRespMount {
		log.Tracef("MatchMountId, comparing device serial no %s with mount return serial no %s", device.SerialNumber, mount.Device.SerialNumber)
		if mount.Device.SerialNumber == device.SerialNumber {
			log.Tracef("mount ID is %s for serial no %s", mount.ID, device.SerialNumber)
			mountId = mount.ID
			break
		}
	}
	return mountId

}

// MountFilesystem calls POST on mounts for chapi to mount volume on host
func (chapiClient *Client) MountFilesystem(volume *model.Volume, mountPoint string) error {
	log.Trace("MountFilesystem called for ", volume.Name, " mountPoint ", mountPoint)

	device, err := chapiClient.GetDeviceFromVolume(volume)
	if err != nil {
		return err
	}
	if device == nil {
		return errors.New("no matching device found")
	}

	//mountpoint is not required here since volume is being formated first time
	var getRespMount []*model.Mount
	err = chapiClient.GetMounts(&getRespMount, device.SerialNumber)
	if err != nil {
		return err
	}
	mountID := MatchMountId(getRespMount, device)
	log.Trace("Before MountID ", mountID)
	if len(mountID) <= 0 {
		log.Trace("No mount point returned by CHAPI for ", volume.Name, " mountPoint ", mountPoint)
		//Construct a mount id in linux way.
		mountID = linux.HashMountID(mountPoint + device.SerialNumber)
	}
	log.Trace("After MountID ", mountID)
	var rspMount model.Mount
	reqMount := model.Mount{
		Mountpoint: mountPoint,
		Device:     device,
		ID:         mountID,
	}

	err = chapiClient.Mount(&reqMount, &rspMount)
	if err != nil {
		log.Trace("Err :", err.Error())
		return err
	}
	return nil
}

// TODO: REMOVE THIS ONCE THE WINDOWS CHAPI IS IN-SYNC WITH LINUX CHAPI ENDPOINT - DeleteDevice
//DetachDevice : delete the os device on the host
// nolint :
func (chapiClient *Client) DetachDevice(device *model.Device) (err error) {
	log.Tracef(">>>>> DetachDevice with %#v", device)
	defer log.Trace("<<<<< DetachDevice")

	// fetch host ID
	err = chapiClient.cacheHostID()
	if err != nil {
		return err
	}

	serialNumber := device.SerialNumber
	deviceURI := fmt.Sprintf(DeviceURIfmt, fmt.Sprintf(HostURIfmt, chapiClient.hostID), serialNumber)

	var deviceResp model.Device
	var errResp *ErrorResponse
	var chapiResp Response
	chapiResp.Data = &deviceResp
	chapiResp.Err = &errResp
	_, err = chapiClient.client.DoJSON(&connectivity.Request{Action: "DELETE", Path: deviceURI, Header: chapiClient.header, Payload: device, Response: &chapiResp, ResponseError: &chapiResp})
	if err != nil {
		if errResp != nil {
			log.Error(errResp.Info)
			return errors.New(errResp.Info)
		}
		log.Errorf("Err :%s", err.Error())
		return err
	}
	return err
}

func createMountDir(mountPoint string) (isDir bool, err error) {
	err = os.MkdirAll(mountPoint, 700)
	// if previous mount has not got cleaned up the mountPoint may already exist. Don't treat it as an error
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "file exists") {
		return false, err
	}
	_, isDir, _ = util.FileExists(mountPoint)
	if isDir == false {
		log.Trace("Failed to created mountpoint" + mountPoint)
		return false, errors.New("failed to create directory at mountpoint " + mountPoint)
	}
	return isDir, nil
}
