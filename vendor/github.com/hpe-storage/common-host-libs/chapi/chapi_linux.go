// (c) Copyright 2018 Hewlett Packard Enterprise Development LP

package chapi

import (
	"github.com/gorilla/mux"
	"github.com/hpe-storage/common-host-libs/connectivity"
	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"

	"net"
	"net/http"
	"os"

	"strconv"
	"sync"
)

var (
	chapidLock     sync.Mutex
	chapidListener net.Listener
)

// NewRouter creates a new mux.Router
func NewRouter() *mux.Router {
	routes := []util.Route{
		util.Route{
			Name:        "Hosts",
			Method:      "GET",
			Pattern:     "/hosts",
			HandlerFunc: getHosts,
		},
		util.Route{
			Name:        "Hosts",
			Method:      "GET",
			Pattern:     "/hosts/{id}",
			HandlerFunc: getHostInfo,
		},
		util.Route{
			Name:        "Devices",
			Method:      "GET",
			Pattern:     "/hosts/{id}/devices",
			HandlerFunc: getDevices,
		},
		util.Route{
			Name:        "Hostname",
			Method:      "GET",
			Pattern:     "/hosts/{id}/hostname",
			HandlerFunc: getHostNameAndDomain,
		},
		util.Route{
			Name:        "CreateDevice",
			Method:      "POST",
			Pattern:     "/hosts/{id}/devices",
			HandlerFunc: createDevices,
		},
		util.Route{
			Name:        "CreateFileSystemOnDevice",
			Method:      "PUT",
			Pattern:     "/hosts/{id}/devices/{serialnumber}/{filesystem}",
			HandlerFunc: createFileSystemOnDevice,
		},
		util.Route{
			Name:        "OfflineDevice",
			Method:      "PUT",
			Pattern:     "/hosts/{id}/devices/{serialnumber}/actions/offline",
			HandlerFunc: offlineDevice,
		},
		util.Route{
			Name:        "DeleteDevice",
			Method:      "DELETE",
			Pattern:     "/hosts/{id}/devices/{serialnumber}",
			HandlerFunc: deleteDevice,
		},
		util.Route{
			Name:        "DeviceWithSerialNumber",
			Method:      "GET",
			Pattern:     "/hosts/{id}/devices/{serialnumber}",
			HandlerFunc: getDeviceForSerialNumber,
		},
		util.Route{
			Name:        "PartitionsForDevice",
			Method:      "GET",
			Pattern:     "/hosts/{id}/devices/{serialnumber}/partitions",
			HandlerFunc: getPartitionsForDevice,
		},
		util.Route{
			Name:        "MountsOnHost",
			Method:      "GET",
			Pattern:     "/hosts/{id}/mounts/{serialNumber}",
			HandlerFunc: getMountsOnHostForSerialNumber,
		},
		util.Route{
			Name:        "MountDevice",
			Method:      "POST",
			Pattern:     "/hosts/{id}/mounts/{serialNumber}",
			HandlerFunc: mountDevice,
		},
		util.Route{
			Name:        "UnmountDevice",
			Method:      "DELETE",
			Pattern:     "/hosts/{id}/mounts/{mountID}",
			HandlerFunc: unmountDevice,
		},
		util.Route{
			Name:        "MountForDevice",
			Method:      "GET",
			Pattern:     "/hosts/{id}/mounts/{mountid}/{serialNumber}",
			HandlerFunc: getMountForDevice,
		},
		util.Route{
			Name:        "HostInitiators",
			Method:      "GET",
			Pattern:     "/hosts/{id}/initiators",
			HandlerFunc: getHostInitiators,
		},
		util.Route{
			Name:        "HostNetworks",
			Method:      "GET",
			Pattern:     "/hosts/{id}/networks",
			HandlerFunc: getHostNetworks,
		},
		util.Route{
			Name:        "Recommendations",
			Method:      "GET",
			Pattern:     "/hosts/{id}/recommendations",
			HandlerFunc: getHostRecommendations,
		},
		util.Route{
			Name:        "DeletingDevices",
			Method:      "GET",
			Pattern:     "/hosts/{id}/deletingdevices",
			HandlerFunc: getDeletingDevices,
		},
		util.Route{
			Name:        "ChapInfo",
			Method:      "GET",
			Pattern:     "/hosts/{id}/chapinfo",
			HandlerFunc: getChapInfo,
		},
	}
	router := mux.NewRouter().StrictSlash(true)
	util.InitializeRouter(router, routes)
	return router
}

// Run will invoke a new chapid listener with socket filename containing current process ID
func Run() (err error) {
	// check if chapid is already running listening on standard socket or per process socket
	if IsChapidRunning(ChapidSocketPath+ChapidSocketName) ||
		IsChapidRunning(ChapidSocketPath+ChapidSocketName+strconv.Itoa(os.Getpid())) {
		// return err as nil to indicate chapid is already running for current process
		return nil
	}

	// acquire lock to avoid multiple chapid servers
	chapidLock.Lock()
	defer chapidLock.Unlock()

	//first check if the directory exists
	_, isdDir, _ := util.FileExists(ChapidSocketPath)
	if !isdDir {
		// create the directory for   chapidSocket
		err = os.MkdirAll(ChapidSocketPath, 0700)
		if err != nil {
			log.Error("Unable to create directory " + ChapidSocketPath + " for chapid routine to run")
			return err
		}
	}

	chapidResult := make(chan error)
	// start chapid server
	go startChapid(chapidResult)
	// wait for the response on channel
	err = <-chapidResult
	return err
}

// This function will invoke a new chapid listener with socket filename containing current process ID
// NOTE: invoke this function as go routine as it will block on socket listener
func startChapid(result chan error) {
	var err error
	// create chapidSocket for listening
	chapidListener, err = net.Listen("unix", ChapidSocketPath+ChapidSocketName+strconv.Itoa(os.Getpid()))
	if err != nil {
		log.Error("listen error, Unable to create ChapidServer ", err.Error())
		result <- err
		return
	}
	router := NewRouter()
	// indicate on channel before we block on listener
	result <- nil
	err = http.Serve(chapidListener, router)
	if err != nil {
		log.Info("exiting chapid server", err.Error())
	}
}

// StopChapid will stop the given http listener
func StopChapid() error {
	chapidLock.Lock()
	defer chapidLock.Unlock()

	// stop the listener
	if chapidListener != nil {
		err := chapidListener.Close()
		if err != nil {
			log.Error("Unable to close chapid listener " + chapidListener.Addr().String())
		}
		chapidListener = nil
		os.RemoveAll(ChapidSocketPath + ChapidSocketName + strconv.Itoa(os.Getpid()))
	}
	return nil
}

// IsChapidRunning return true if chapid is running as part of service listening on given socket
func IsChapidRunning(chapidSocket string) bool {
	chapidLock.Lock()
	defer chapidLock.Unlock()

	//first check if the chapidSocket file exists
	isPresent, _, _ := util.FileExists(chapidSocket)
	if !isPresent {
		return false
	}
	var chapiResp Response
	// generate chapid client for default daemon
	var errResp *ErrorResponse
	chapiResp.Err = &errResp
	chapiClient := connectivity.NewSocketClient(chapidSocket)
	if chapiClient != nil {
		_, err := chapiClient.DoJSON(&connectivity.Request{Action: "GET", Path: "/hosts", Payload: nil, Response: &chapiResp, ResponseError: &chapiResp})
		if errResp != nil {
			log.Error(errResp.Info)
			return false
		}
		if err == nil {
			return true
		}
	}
	return false
}

// RunNimbled :
func RunNimbled(c chan error) {
	err := cleanupExistingSockets()
	if err != nil {
		log.Fatal("Unable to cleanup existing sockets")
	}
	//1. first check if the directory exists
	_, isdDir, _ := util.FileExists(ChapidSocketPath)
	if !isdDir {
		// create the directory for   chapidSocket
		err = os.MkdirAll(ChapidSocketPath, 0700)
		if err != nil {
			log.Fatal("Unable to create directory " + ChapidSocketPath + " for chapid server to run")
		}
	}

	//create chapidSocket for listening
	chapidSocket, err := net.Listen("unix", ChapidSocketPath+ChapidSocketName)
	if err != nil {
		log.Fatal("listen error, Unable to create ChapidServer ", err)
	}

	router := NewRouter()
	go runNimbled(chapidSocket, router, c)

}

func runNimbled(l net.Listener, m *mux.Router, c chan error) {
	log.Trace("Serving socket :", l.Addr().String())
	c <- http.Serve(l, m)

	// close the socket
	log.Tracef("closing the socket %v", l.Addr().String())
	defer l.Close()
	//cleanup the socket file
	defer os.RemoveAll(ChapidSocketPath + ChapidSocketName)
}

// cleanup the existing unix sockets before creating them again
func cleanupExistingSockets() (err error) {
	log.Trace("Cleaning up existing socket")
	//clean up chapid socket
	isPresent, _, err := util.FileExists(ChapidSocketPath + ChapidSocketName)
	if err != nil {
		log.Trace("err", err)
		return err
	}
	if isPresent {
		err = os.RemoveAll(ChapidSocketPath + ChapidSocketName)
		if err != nil {
			return err
		}
	}
	return nil
}

// CheckFsCreationInProgress checks if mkfs process is using the device
func CheckFsCreationInProgress(device model.Device) (inProgress bool, err error) {
	return linux.CheckFsCreationInProgress(device)
}
