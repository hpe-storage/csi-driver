// (c) Copyright 2018 Hewlett Packard Enterprise Development LP

package chapi

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hpe-storage/common-host-libs/connectivity"
	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
)

// GetSocketName returns unix socket name (per process)
func GetSocketName() string {
	var socketName string
	if IsChapidRunning(ChapidSocketPath + ChapidSocketName) {
		socketName = ChapidSocketPath + ChapidSocketName
	} else {
		socketName = ChapidSocketPath + ChapidSocketName + strconv.Itoa(os.Getpid())
	}
	return socketName
}

// NewChapiSocketClient to create chapi socket client
func NewChapiSocketClient() (*Client, error) {
	var chapiClient *Client

	socketName := GetSocketName()
	log.Traceln("setting up chapi client with socket", socketName)
	// setup chapi client with timeout
	socketClient := connectivity.NewSocketClient(socketName)
	chapiClient = &Client{client: socketClient, socket: socketName}
	return chapiClient, nil
}

// NewChapiSocketClientWithTimeout to create chapi socket client with timeout
func NewChapiSocketClientWithTimeout(timeout time.Duration) (*Client, error) {
	var chapiClient *Client

	socketName := GetSocketName()
	log.Traceln("setting up chapi client with socket", socketName, "and timeout", timeout)
	// setup chapi client with timeout
	socketClient := connectivity.NewSocketClientWithTimeout(socketName, timeout)
	chapiClient = &Client{client: socketClient, socket: socketName}
	return chapiClient, nil
}

// TODO: Consider removing this function after merging to default

// NewChapiClientWithTimeout returns the chapi client with timeout configuration that communicates over the required unix socket(per process or common service)
func NewChapiClientWithTimeout(timeout time.Duration) (*Client, error) {
	var chapiClient *Client
	var err error

	chapiClient, err = NewChapiSocketClientWithTimeout(timeout)
	if err != nil {
		return nil, err
	}
	return chapiClient, nil
}

// NewChapiClient returns the chapi client that communicates over the required unix socket(per process or common service)
func NewChapiClient() (*Client, error) {
	var chapiClient *Client
	var err error

	// Default chapi client is based on unix socket

	chapiClient, err = NewChapiSocketClient()
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
	var hostnameURI string
	var chapiResp Response
	var errResp *ErrorResponse
	chapiResp.Err = &errResp

	chapiResp.Data = &host
	hostnameURI = fmt.Sprintf(HostnameURIfmt, fmt.Sprintf(HostURIfmt, chapiClient.hostID))

	log.Tracef("GetHostName called with URI %s", hostnameURI)
	_, err = chapiClient.client.DoJSON(&connectivity.Request{Action: "GET", Path: hostnameURI, Header: chapiClient.header, Payload: nil, Response: &chapiResp, ResponseError: &chapiResp})
	if err != nil {
		if errResp != nil {
			log.Error(errResp.Info)
			return nil, errors.New(errResp.Info)
		}
		log.Trace("Err :", err.Error())
		return nil, err
	}

	return host, nil
}

// SetupFilesystemAndPermissions creates filesystem and applies permissions if present
func (chapiClient *Client) SetupFilesystemAndPermissions(device *model.Device, vol *model.Volume, filesystem string) error {
	log.Tracef("SetupFilesystemAndPermissions called for volume (%s), mountpoint(%s)", vol.Name, vol.MountPoint)

	err := chapiClient.CreateFilesystem(device, vol, filesystem)
	if err != nil {
		return fmt.Errorf("unable to create filesystem %s", err.Error())
	}
	var reqMount, respMount *model.Mount
	mountID := linux.HashMountID(vol.MountPoint + device.SerialNumber)
	log.Trace("MountID :", mountID)
	reqMount = &model.Mount{
		Mountpoint: vol.MountPoint,
		Device:     device,
		ID:         mountID,
	}

	//make sure the mountpoint is created
	_, err = linux.CreateMountDir(vol.MountPoint)
	if err != nil {
		return err
	}
	err = chapiClient.Mount(reqMount, respMount)
	if err != nil {
		log.Trace("Err :", err.Error())
		return err
	}

	// only if filesystem options are present, apply them on mount
	if mode, ok := vol.Status[model.FsModeOpt]; ok {
		err = linux.ChangeMode(vol.MountPoint, mode.(string))
		if err != nil {
			log.Errorf("unable to update the filesystem mode for device %s to %s (%s)", device.AltFullPathName, mode, err.Error())
		}
	}
	if owner, ok := vol.Status[model.FsOwnerOpt]; ok {
		userGroup := strings.Split(owner.(string), ":")
		if len(userGroup) > 1 {
			err := linux.ChangeOwner(vol.MountPoint, userGroup[0], userGroup[1])
			if err != nil {
				log.Errorf("unable to change ownership to %v for mountPoint %s (%s)", userGroup, vol.MountPoint, err.Error())
			}
		}
	}
	// do not unmount here, return with mountpoint
	return nil
}

// MountFilesystem calls POST on mounts for chapi to mount volume on host
func (chapiClient *Client) MountFilesystem(volume *model.Volume, mountPoint string) error {
	log.Trace("MountFilesystem called for ", volume.Name, " mountPoint ", mountPoint)

	device, err := chapiClient.GetDeviceFromVolume(volume)
	if err != nil {
		return err
	}
	if device == nil {
		return fmt.Errorf("no matching device found for volume %s", volume.Name)
	}
	if device != nil && device.State != model.ActiveState.String() {
		return fmt.Errorf("device +%v does not have active paths. state=%s. Failing request", device, device.State)
	}
	mountID := linux.HashMountID(mountPoint + device.SerialNumber)
	log.Trace("MountID :", mountID)
	var rspMount model.Mount
	reqMount := model.Mount{
		Mountpoint: mountPoint,
		Device:     device,
		ID:         mountID,
	}

	//make sure the mountpoint is created
	_, err = linux.CreateMountDir(mountPoint)
	if err != nil {
		return err
	}
	err = chapiClient.Mount(&reqMount, &rspMount)
	if err != nil {
		log.Trace("Err :", err.Error())
		return err
	}
	return nil
}
