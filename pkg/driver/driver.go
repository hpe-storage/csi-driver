// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Scalingo/go-etcd-lock/lock"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hpe-storage/common-host-libs/chapi"
	"github.com/hpe-storage/common-host-libs/concurrent"
	"github.com/hpe-storage/common-host-libs/dbservice"
	"github.com/hpe-storage/common-host-libs/dbservice/etcd"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/storageprovider"
	"github.com/hpe-storage/common-host-libs/storageprovider/csp"
	"github.com/hpe-storage/common-host-libs/util"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/flavor/kubernetes"
	"github.com/hpe-storage/csi-driver/pkg/flavor/vanilla"
	"github.com/hpe-storage/csi-driver/pkg/monitor"
)

const (
	defaultTTL = 60
)

// Driver is the object that implements the CSI interfaces
type Driver struct {
	name     string
	version  string
	endpoint string

	chapiDriver      chapi.Driver
	storageProviders map[string]storageprovider.StorageProvider
	flavor           flavor.Flavor
	grpc             NonBlockingGRPCServer
	podMonitor       *monitor.Monitor

	controllerServiceCapabilities     []*csi.ControllerServiceCapability
	nodeServiceCapabilities           []*csi.NodeServiceCapability
	volumeCapabilityAccessModes       []*csi.VolumeCapability_AccessMode
	pluginVolumeExpansionCapabilities []*csi.PluginCapability_VolumeExpansion

	requestCache      map[string]interface{}
	requestCacheMutex *concurrent.MapMutex
	DBService         dbservice.DBService
}

// NewDriver returns a driver that implements the gRPC endpoints required to support CSI
func NewDriver(name, version, endpoint, flavorName string, nodeService bool, dbServer string, dbPort string, podMonitor bool, podMonitorInterval int64) (*Driver, error) {

	// Get CSI driver
	driver := getDriver(name, version, endpoint)

	// Configure flavor
	if flavorName == flavor.Kubernetes {
		flavor, err := kubernetes.NewKubernetesFlavor(nodeService, driver.chapiDriver)
		if err != nil {
			return nil, err
		}
		driver.flavor = flavor
	} else {
		driver.flavor = &vanilla.Flavor{}
	}

	if podMonitor {
		driver.podMonitor = monitor.NewMonitor(driver.flavor, podMonitorInterval)
	}

	// Init Controller Service Capabilities supported by the driver
	driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		//ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	})

	// Init Node Service Capabilities supported by the driver
	driver.AddNodeServiceCapabilities([]csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
	})

	// Init Volume Expansion Capabilities supported by the driver
	driver.AddPluginCapabilityVolumeExpansion([]csi.PluginCapability_VolumeExpansion_Type{
		csi.PluginCapability_VolumeExpansion_ONLINE,
	})

	// Init Volume Capabilities supported by the driver
	driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,       // Single-Node Single-Writer Mode
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,  // Single-Node Single-Reader Mode
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,   // Multi-Node Multi-Reader Mode (Read-Only)
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER, // Multi-Node Single-Writer Mode
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,  // Multi-Node Multi-Writer Mode
	})

	// Init DB service client instance if DB server name is specified
	if dbServer != "" {
		var err error
		// Get DB client instance
		driver.DBService, err = getDBClient(dbServer, dbPort)
		if err != nil {
			return nil, err
		}
	}
	if driver.DBService == nil {
		log.Info("DB service disabled!!!")
	} else {
		log.Info("DB service enabled!!!")
	}

	return driver, nil
}

// getDBClient instance for the given server and port
func getDBClient(server string, port string) (*etcd.DBClient, error) {
	log.Tracef(">>>>> getDBClient, server: %s, port: %s", server, port)
	defer log.Trace("<<<<< getDBClient")

	endPoints := []string{fmt.Sprintf("%s:%s", server, port)}
	// Create DB service client
	dbClient, err := etcd.NewClient(endPoints, etcd.DefaultVersion)
	if err != nil {
		log.Error("DB server might not be running or not reachable, err: ", err.Error())
		return nil, fmt.Errorf("Failed to get DB client instance, err: %s", err.Error())
	}
	return dbClient, nil
}

// Start starts the gRPC server
func (driver *Driver) Start(nodeService bool) error {
	go func() {
		driver.grpc = NewNonBlockingGRPCServer()
		if nodeService {
			driver.grpc.Start(driver.endpoint, driver, nil, driver)
		} else {
			driver.grpc.Start(driver.endpoint, driver, driver, nil)
			// start pod monitor along with controller plugin
			if driver.podMonitor != nil {
				driver.podMonitor.StartMonitor()
			}
		}
	}()
	return nil
}

// Stop stops the gRPC server
func (driver *Driver) Stop(nodeService bool) error {
	driver.grpc.GracefulStop()
	if nodeService {
		driver.flavor.UnloadNodeInfo()
	}
	if driver.podMonitor != nil {
		driver.podMonitor.StopMonitor()
	}
	return nil
}

// StartScrubber starts the scrubber period task
func (driver *Driver) StartScrubber(nodeService bool) chan bool {
	if nodeService {
		// Fetch scrubber interval from env variable. If unspecified, then use default value.
		scrubberInterval := driver.nodeGetIntEnv(inlineVolumeScrubberIntervalKey)
		if scrubberInterval <= 0 {
			scrubberInterval = defaultInlineVolumeScrubberInterval
			log.Tracef("Using defaultInlineVolumeScrubberInterval %d", scrubberInterval)
		}
		// Fetch PODs directory path from env variable. If unspecified, then use default value.
		podsDirPath := os.Getenv(podsDirPathKey)
		if podsDirPath == "" {
			podsDirPath = defaultPodsDirPath
			log.Tracef("Using defaultPodsDirPath %s", podsDirPath)
		}
		log.Infof("Scheduled ephemeral inline volumes scrubber task to run every %d seconds, PodsDirPath: [%s]", scrubberInterval, podsDirPath)
		tick := time.NewTicker(time.Duration(scrubberInterval) * time.Second)
		done := make(chan bool)
		go func() {
			// Cleanup ephemeral orphan volumes
			driver.ScrubberTask(time.Now(), podsDirPath)
			for {
				select {
				case t := <-tick.C:
					driver.ScrubberTask(t, podsDirPath)
				case d := <-done:
					log.Tracef("Closed the scrubber task channel: [%v]", d)
					return
				}
			}
		}()
		return done
	}
	return nil
}

// StopScrubber stops the scrubber period task
func (driver *Driver) StopScrubber(nodeService bool, chanDone chan<- bool) error {
	if nodeService {
		chanDone <- true // Send true to close the channel
		log.Infof("Stopped the scrubber task at %v", time.Now())
	}
	return nil
}

// AddControllerServiceCapabilities configures the service capabilities returned by the controller service
// nolint: dupl
func (driver *Driver) AddControllerServiceCapabilities(capabilities []csi.ControllerServiceCapability_RPC_Type) {
	var controllerServiceCapabilities []*csi.ControllerServiceCapability

	for _, c := range capabilities {
		log.Infof("Enabling controller service capability: %v", c.String())
		controllerServiceCapabilities = append(controllerServiceCapabilities, NewControllerServiceCapability(c))
	}

	driver.controllerServiceCapabilities = controllerServiceCapabilities
}

// AddNodeServiceCapabilities configures the service capabilities returned by the node service
// nolint: dupl
func (driver *Driver) AddNodeServiceCapabilities(capabilities []csi.NodeServiceCapability_RPC_Type) {
	var nodeServiceCapabilities []*csi.NodeServiceCapability

	for _, c := range capabilities {
		log.Infof("Enabling node service capability: %v", c.String())
		nodeServiceCapabilities = append(nodeServiceCapabilities, NewNodeServiceCapability(c))
	}

	driver.nodeServiceCapabilities = nodeServiceCapabilities
}

// AddPluginCapabilityVolumeExpansion returns the plugin volume expansion capabilities
// nolint: dupl
func (driver *Driver) AddPluginCapabilityVolumeExpansion(expansionTypes []csi.PluginCapability_VolumeExpansion_Type) {
	var pluginVolumeExpansionCapabilities []*csi.PluginCapability_VolumeExpansion

	for _, t := range expansionTypes {
		log.Infof("Enabling volume expansion type: %v", t.String())
		pluginVolumeExpansionCapabilities = append(pluginVolumeExpansionCapabilities, NewPluginCapabilityVolumeExpansion(t))
	}
	driver.pluginVolumeExpansionCapabilities = pluginVolumeExpansionCapabilities
}

// AddVolumeCapabilityAccessModes returns the volume capability access modes
// nolint: dupl
func (driver *Driver) AddVolumeCapabilityAccessModes(accessModes []csi.VolumeCapability_AccessMode_Mode) {
	var volumeCapabilityAccessModes []*csi.VolumeCapability_AccessMode

	for _, accessMode := range accessModes {
		log.Infof("Enabling volume access mode: %v", accessMode.String())
		volumeCapabilityAccessModes = append(volumeCapabilityAccessModes, NewVolumeCapabilityAccessMode(accessMode))
	}

	driver.volumeCapabilityAccessModes = volumeCapabilityAccessModes
}

// IsSupportedMultiNodeAccessMode returns true if given capabilities have accessmode of supported multi-node types
func (driver *Driver) IsSupportedMultiNodeAccessMode(capabilities []*csi.VolumeCapability) bool {
	for _, volCap := range capabilities {
		switch volCap.GetAccessMode().GetMode() {
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			return true
		}
		return false
	}
	return false
}

// IsReadOnlyAccessMode returns true if accessmode is ReadOnly for single and multi-node access
func (driver *Driver) IsReadOnlyAccessMode(capabilities []*csi.VolumeCapability) bool {
	for _, volCap := range capabilities {
		switch volCap.GetAccessMode().GetMode() {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			return true
		}
	}
	return false
}

// AddStorageProvider adds a storage provider to the driver
func (driver *Driver) AddStorageProvider(credentials *storageprovider.Credentials) error {
	log.Trace(">>>>> AddStorageProvider")
	defer log.Trace("<<<<< AddStorageProvider")

	log.Infof("Adding connection to CSP at IP %s, port %d, context path %s, with username %s and serviceName %s",
		credentials.Backend, credentials.ServicePort, credentials.ContextPath, credentials.Username, credentials.ServiceName)

	// Get CSP instance
	csp, err := csp.NewContainerStorageProvider(credentials)
	if err != nil {
		log.Errorf("Failed to create new CSP connection from given parameters")
		return err
	}

	driver.storageProviders[driver.GenerateStorageProviderCacheKey(credentials)] = csp
	log.Tracef("Number of cached/known storage providers: %d", len(driver.storageProviders))
	return nil
}

// RemoveStorageProvider removes a storage provider from the driver
func (driver *Driver) RemoveStorageProvider(credentials *storageprovider.Credentials) {
	cacheKey := driver.GenerateStorageProviderCacheKey(credentials)
	if _, ok := driver.storageProviders[cacheKey]; ok {
		delete(driver.storageProviders, cacheKey)
	}
}

// GetStorageProvider gets the storage provider given the map of secrets
// nolint: misspell
func (driver *Driver) GetStorageProvider(secrets map[string]string) (storageprovider.StorageProvider, error) {
	log.Trace(">>>>> GetStorageProvider")
	defer log.Trace("<<<<< GetStorageProvider")

	// Create credentails
	credentials, err := storageprovider.CreateCredentials(secrets)
	if err != nil {
		log.Errorf("Failed to create credentials, err: %s", err.Error())
		return nil, err
	}

	cacheKey := driver.GenerateStorageProviderCacheKey(credentials)
	if csp, ok := driver.storageProviders[cacheKey]; ok {
		// TODO: verify other properties (username, password) of the CSP have not changed that would result in an update
		log.Tracef("Storage provider already exists. Returning it.")
		return csp, nil
	}

	err = driver.AddStorageProvider(credentials)
	if err != nil {
		return nil, err
	}

	return driver.storageProviders[cacheKey], nil
}

// GenerateStorageProviderCacheKey generates unique hash for the credential pair {Backend, Username}
func (driver *Driver) GenerateStorageProviderCacheKey(credentials *storageprovider.Credentials) string {
	result := sha256.Sum256([]byte(fmt.Sprintf("%s%s", credentials.Backend, credentials.Username)))
	return fmt.Sprintf("%x", result)
}

// IsSupportedPluginVolumeExpansionCapability returns true if the given volume expansion capability is supported else returns false
// nolint dupl
func (driver *Driver) IsSupportedPluginVolumeExpansionCapability(capType csi.PluginCapability_VolumeExpansion_Type) bool {
	for _, cap := range driver.pluginVolumeExpansionCapabilities {
		if cap.GetType() == capType {
			return true
		}
	}
	return false
}

// IsSupportedControllerCapability returns true if the given capability is supported else returns false
func (driver *Driver) IsSupportedControllerCapability(capType csi.ControllerServiceCapability_RPC_Type) bool {
	for _, cap := range driver.controllerServiceCapabilities {
		if cap.GetRpc().Type == capType {
			return true
		}
	}
	return false
}

// IsSupportedNodeCapability returns true if the given node capability is supported else returns false
func (driver *Driver) IsSupportedNodeCapability(capType csi.NodeServiceCapability_RPC_Type) bool {
	for _, cap := range driver.nodeServiceCapabilities {
		if cap.GetRpc().Type == capType {
			return true
		}
	}
	return false
}

// GetVolumeByID retrieves the volume instance from the CSP if exists
func (driver *Driver) GetVolumeByID(id string, secrets map[string]string) (*model.Volume, error) {
	log.Trace(">>>>> GetVolume, ID: ", id)
	defer log.Trace("<<<<< GetVolume")

	var volume *model.Volume
	var err error
	// When secrets specified
	if len(secrets) != 0 {
		log.Tracef("Secrets are provided. Checking with this particular storage provider.")

		// Get Storage Provider
		storageProvider, err := driver.GetStorageProvider(secrets)
		if err != nil {
			log.Error("err: ", err.Error())
			return nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to get storage provider from secrets, %s", err.Error()))
		}

		// check if the volume exists
		volume, err = storageProvider.GetVolume(id)
		if err != nil {
			log.Error("err: ", err.Error())
			return nil, status.Error(codes.Unavailable, fmt.Sprintf("Error while attempting to get volume with ID %s, %s", id, err.Error()))
		}
	} else {
		log.Tracef("Secrets not provided. Checking all known storage providers.")

		// Search the volume in each known storage provider
		for _, storageProvider := range driver.storageProviders {
			volume, err = storageProvider.GetVolume(id)
			if err != nil {
				log.Error("err: ", err.Error())
				return nil, status.Error(codes.Unavailable, fmt.Sprintf("Error while attempting to get volume with ID %s, %s", id, err.Error()))
			}
		}
	}
	if volume == nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Volume with ID %s not found", id))
	}

	log.Tracef("Found Volume %+v", volume)
	return volume, nil
}

// GetVolumeByName retrieves the volume instance by name from the CSP if exists
func (driver *Driver) GetVolumeByName(name string, secrets map[string]string) (*model.Volume, error) {
	log.Trace(">>>>> GetVolumeByName, name: ", name)
	defer log.Trace("<<<<< GetVolumeByName")

	var volume *model.Volume
	var err error

	// Get Storage Provider
	storageProvider, err := driver.GetStorageProvider(secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to get storage provider from secrets, %s", err.Error()))
	}

	// check if the volume exists
	volume, err = storageProvider.GetVolumeByName(name)
	if err != nil {
		log.Error("err: ", err.Error())
		return nil, status.Error(codes.Unavailable, fmt.Sprintf("Error while attempting to get volume %s, %s", name, err.Error()))
	}

	log.Tracef("Found Volume %+v", volume)
	return volume, nil
}

// DeleteVolumeByName deletes the volume by name. If force is true, then destroys it immediately.
func (driver *Driver) DeleteVolumeByName(name string, secrets map[string]string, force bool) error {
	// Get the volume if exists
	volume, err := driver.GetVolumeByName(name, secrets)
	if err != nil {
		log.Error("err: ", err.Error())
		return err
	}
	if volume != nil {
		// Destroy volume if exists
		err = driver.deleteVolume(volume.ID, secrets, force)
		if err != nil {
			log.Errorf("Error destroying the volume %s with ID %s, err: %s",
				name, volume.ID, err.Error())
			return err
		}
	}
	return nil
}

// HandleDuplicateRequest checks for the duplicate request in the cache map.
// If yes, then returns ABORTED error, else inserts the entry into cache map and returns nil
func (driver *Driver) HandleDuplicateRequest(key string) error {
	log.Trace(">>>>> HandleDuplicateRequest, key: ", key)
	defer log.Trace("<<<<< HandleDuplicateRequest")

	// When no DB support, use local in-memory map
	//	- The driver's in-memory map is used to cache the requests
	if driver.DBService == nil {

		// Look for the key entry in the cache map
		value := driver.GetRequest(key)
		if value != nil {
			return status.Error(
				codes.Aborted,
				fmt.Sprintf("There is already an operation pending for the specified id %s", key),
			)
		}
		// Insert the key entry to the cache map
		driver.AddRequest(key, true)
		return nil
	}

	// With DB support
	//	- Check for the lock status of the key.
	//	- Acquire the lock on the key in the Database.
	//	- Persist the lock entry in the map cache (in-memory).
	//  - The caller's responsibility (defer action) is to remove the above entries once operation is completed.
	locked, err := driver.DBService.IsLocked(key)
	if err != nil {
		return status.Error(
			codes.Internal,
			fmt.Sprintf("Error while looking for the key '%s' in the DB, err: %s", key, err.Error()),
		)
	}
	// If already in locked state, then return Aborted error
	if locked {
		return status.Error(
			codes.Aborted,
			fmt.Sprintf("There is already an operation pending for the specified id %s", key),
		)
	}
	log.Tracef("About to acquire DB lock on the key '%s'", key)
	// Get the exclusive lock on the key
	lck, err := driver.DBService.WaitAcquireLock(key, defaultTTL)
	if err != nil {
		return status.Error(
			codes.Internal,
			fmt.Sprintf("Error while acquiring DB lock on the key '%s', err: %s", key, err.Error()),
		)
	}
	// Insert the DB lock entry to the cache map
	driver.AddRequest(key, lck)
	return nil
}

/***************************** In-Memory Cache Map Operations *****************************/

// GetRequest retrieves the value in the driver cache map. If not exists, returns nil
func (driver *Driver) GetRequest(key string) interface{} {
	log.Trace(">>>>> GetRequest, key: ", key)
	defer log.Trace("<<<<< GetRequest")

	// Get the mutex lock on the map for the key
	driver.requestCacheMutex.Lock(key)
	defer driver.requestCacheMutex.Unlock(key)

	return driver.requestCache[key]
}

// AddRequest inserts the request entry into the driver cache map
func (driver *Driver) AddRequest(key string, value interface{}) {
	log.Tracef(">>>>> AddRequest, key: %s, value: %v", key, value)
	defer log.Trace("<<<<< AddRequest")

	// Get the mutex lock on the map for the key
	driver.requestCacheMutex.Lock(key)
	defer driver.requestCacheMutex.Unlock(key)

	driver.requestCache[key] = value
	log.Tracef("Print RequestCache: %v", driver.requestCache) // Debug
	log.Tracef("Successfully inserted an entry with key %s to the cache map", key)
}

// ClearRequest removes the request entry from the driver cache map
func (driver *Driver) ClearRequest(key string) {
	log.Trace(">>>>> ClearRequest, key: ", key)
	defer log.Trace("<<<<< ClearRequest")

	// Get the mutex lock on the map for the key
	driver.requestCacheMutex.Lock(key)
	defer driver.requestCacheMutex.Unlock(key)

	if driver.DBService != nil {
		value := driver.requestCache[key]
		if value != nil {
			log.Tracef("About to release DB lock on the key '%s'", key)
			// Release lock in the DB
			if err := driver.DBService.ReleaseLock(value.(lock.Lock)); err != nil {
				log.Errorf("Error while releasing DB lock on the key '%s', err: %s", key, err.Error())
				// Note: Don't return error here as the lock will be released automatically once the ttl expires.
			}
		}
	}
	// Remove the entry from the cache map
	delete(driver.requestCache, key)
	log.Tracef("Print RequestCache: %v", driver.requestCache) // Trace
	log.Tracef("Successfully removed an entry with key %s from the cache map", key)
}

func getString(value interface{}) string {
	switch value.(type) {
	case string:
		return value.(string)
	default: // JSON
		bytes, _ := json.Marshal(value)
		return string(bytes)
	}
}

/******************************************************************************************/

/************************************ DATABASE OPERATIONS *********************************/

// AddToDB creates an entry in the DB for the given key-value pair
func (driver *Driver) AddToDB(key string, value interface{}) error {
	if driver.DBService == nil {
		log.Trace("DB service disabled")
		return nil
	}
	// Create DB entry
	err := driver.DBService.Put(key, getString(value))
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("Error while handling the DB service, err: %v", err.Error()))
	}
	log.Tracef("Added entry for key '%v' in the DB", key)
	return nil
}

// UpdateDB overwrites the entry in the DB with the given key-value pair
func (driver *Driver) UpdateDB(key string, value interface{}) error {
	if driver.DBService == nil {
		log.Trace("DB service disabled")
		return nil
	}

	// Check if the entry exists. If not, then return error
	oldValue, err := driver.DBService.Get(key)
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("Error while handling the DB service, err: %v", err.Error()))
	}
	if oldValue == nil {
		return status.Error(codes.Internal, fmt.Sprintf("DB entry for key '%v' not found, update failed", key))
	}

	// Update DB entry
	err = driver.DBService.Put(key, getString(value))
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("Error while handling the DB service, err: %v", err.Error()))
	}
	log.Tracef("Updated entry for key '%v' in the DB", key)
	return nil
}

// RemoveFromDB removes the entry from the DB for the given key
func (driver *Driver) RemoveFromDB(key string) error {
	if driver.DBService == nil {
		log.Trace("DB service disabled")
		return nil
	}
	// Remove the entry from DB
	err := driver.DBService.Delete(key)
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("Error while handling the DB service, err: %v", err.Error()))
	}
	log.Tracef("Removed entry for key '%v' from the DB", key)
	return nil
}

// RemoveFromDBIfPending checks if the entry has value as "PENDING" for the given key.
// If yes, then removes the entry from the DB, else do nothing.
func (driver *Driver) RemoveFromDBIfPending(key string) error {
	if driver.DBService == nil {
		log.Trace("DB service disabled")
		return nil
	}

	// Get entry from DB
	value, err := driver.DBService.Get(key)
	if err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("Error while handling the DB service, err: %v", err.Error()))
	}

	// If entry found and value is PENDING, then remove it from the DB
	if value != nil && strings.EqualFold(*value, Pending) {
		// Remove the entry from DB
		err := driver.DBService.Delete(key)
		if err != nil {
			return status.Error(codes.Internal, fmt.Sprintf("Error while handling the DB service, err: %v", err.Error()))
		}
		log.Tracef("Removed pending entry for key '%v' from the DB", key)
	}
	return nil
}

func (driver *Driver) IsNFSResourceRequest(parameters map[string]string) bool {
	nfsResources := ""
	// Fetch properties for NFS resource creation
	if _, ok := parameters[nfsResourcesKey]; ok {
		nfsResources = parameters[nfsResourcesKey]
	}

	nfsPVC := "false"
	// Check if this is an underlying NFS PVC creation
	if _, ok := parameters[nfsPVCKey]; ok {
		nfsPVC = parameters[nfsPVCKey]
	}

	if nfsResources == "true" && nfsPVC != "true" {
		// this is the original pvc
		return true
	}
	return false
}

// ScrubberTask executes the scrubber function
func (driver *Driver) ScrubberTask(t time.Time, podsDirPath string) error {
	log.Infof(">>>>> Scrubber task invoked at %v", t)
	if err := driver.ScrubEphemeralPods(podsDirPath); err != nil {
		log.Error(err.Error())
		// Log error and continue
	}
	log.Infof("<<<<< Scrubber task completed at %v", time.Now())
	return nil
}

// ScrubEphemeralPods to cleanup the orphan/stale ephemeral volumes and its associated staged devices
func (driver *Driver) ScrubEphemeralPods(podsDirPath string) error {
	log.Trace(">>>>> ScrubEphemeralPods")
	defer log.Trace("<<<<< ScrubEphemeralPods")

	// Check if pods path exists
	exists, isDir, err := util.FileExists(podsDirPath)
	if err != nil {
		return err
	}
	if !exists || !isDir {
		return fmt.Errorf("Path [%s] does not exist or not a directory", podsDirPath)
	}

	// Fetch all ephemeral pods and populate it
	//ephemeralPods := map[string][]*StagingDeviceEphemeralData{}
	ephemeralPods := map[string][]*VolumeHandleTargetPath{}
	err = filepath.Walk(podsDirPath, func(fileFullPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			log.Errorf("Error while processing the path [%s], %s", fileFullPath, walkErr.Error())
			return walkErr
		}
		if info == nil {
			log.Warnf("FileInfo is nil, Skipping directory path [%s]", fileFullPath)
			return filepath.SkipDir /* Skip processing current directory and continue processing other directories */
		}

		// Process only 'ephemeral_data.json' files
		if !info.IsDir() && info.Name() == ephemeralDataFileName {
			log.Tracef("Found Ephemeral data file [%s]", fileFullPath)
			targetDirPath := filepath.Dir(fileFullPath)
			targetPath := fmt.Sprintf("%s/%s", targetDirPath, "mount")

			// Load Ephemeral Data
			ephemeralData, err := loadEphemeralData(targetDirPath, ephemeralDataFileName)
			if err != nil {
				log.Errorf("Failed to load ephemeral data from %s, %s", ephemeralDataFileName, err.Error())
				return nil
			}
			if ephemeralData == nil {
				log.Infof("Missing ephemeral data in %s, skip processing", fileFullPath)
				return nil
			}

			// Add volumePath to the pod list
			volHandleTargetPath := &VolumeHandleTargetPath{
				VolumeHandle: ephemeralData.VolumeHandle,
				TargetPath:   targetPath,
			}
			log.Tracef("Scrub ephemeral inline volume, [POD: %s], [VolumeHandle: %s], [VolumeID: %s], [TargetPath: %s]",
				ephemeralData.PodData.UID, ephemeralData.VolumeHandle, ephemeralData.VolumeID, targetPath)
			ephemeralPods[ephemeralData.PodData.UID] = append(ephemeralPods[ephemeralData.PodData.UID], volHandleTargetPath)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Error processing the pods directory [%s], %s", podsDirPath, err.Error())
	}

	if len(ephemeralPods) == 0 {
		log.Info("No ephemeral inline volumes found")
		return nil
	}
	for podUID, volumePaths := range ephemeralPods {
		// Check if the pod exists on the cluster
		exists, err := driver.flavor.IsPodExists(podUID)
		if err != nil {
			log.Errorf("Failed to process POD [%s], err: %s", podUID, err.Error())
			continue
		}
		if !exists {
			log.Infof("POD with UID %s does not exist. So, attempting to cleanup device(s) and associated ephemeral volume(s)", podUID)
			// Perform nodeUnpublishEphemeralVolume() for each target path
			for _, volumePath := range volumePaths {
				log.Infof("Scrubber attempting to cleanup ephemeral volume %s on target path [%s]",
					volumePath.VolumeHandle, volumePath.TargetPath)
				err := driver.nodeUnpublishEphemeralVolume(volumePath.VolumeHandle, volumePath.TargetPath)
				if err != nil {
					log.Errorf("Failed to node unpublish ephemeral volume from targetPath [%s], err: %s",
						volumePath.TargetPath, err.Error())
					continue
				}
				log.Infof("Scrubber nodeUnpublished ephemeral volume %s from targetPath [%s] successfully",
					volumePath.VolumeHandle, volumePath.TargetPath)
			}
		}
	}
	return nil
}

/******************************************************************************************/
