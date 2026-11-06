// Copyright 2019 Hewlett Packard Enterprise Development LP

package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/hpe-storage/common-host-libs/dbservice/etcd"
	"github.com/hpe-storage/common-host-libs/linux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/tunelinux"
	"github.com/hpe-storage/common-host-libs/util"

	"github.com/hpe-storage/csi-driver/pkg/driver"
	"github.com/hpe-storage/csi-driver/pkg/flavor"
	"github.com/hpe-storage/csi-driver/pkg/nodeinit"
)

const (
	csiVersion           = "1.3"
	csiDriverName        = "csi.hpe.com"
	csiControllerLogFile = "/var/log/hpe-csi-controller.log"
	csiNodeLogFile       = "/var/log/hpe-csi-node.log"
	csiEndpoint          = "unix:///var/lib/kubelet/csi.hpe.com/csi.sock"
)

var (
	// Flag variables for the command options
	name                string
	endpoint            string
	dbServer            string
	dbPort              string
	flavorName          string
	podMonitorInterval  string
	nodeMonitorInterval string

	// RootCmd is the main CSI command
	RootCmd = &cobra.Command{
		Use:              "csi",
		Short:            "HPE CSI command-line utility",
		Long:             `A command-line utility for managing the HPE Container Storage Interface (CSI) plugin`,
		TraverseChildren: true,
		Run: func(cmd *cobra.Command, args []string) {
			isNode, _ := cmd.Flags().GetBool("node-service")
			if isNode {
				log.InitLogging(csiNodeLogFile, nil, true)
			} else {
				log.InitLogging(csiControllerLogFile, nil, true)
			}
			log.Info("**********************************************")
			log.Info("*************** HPE CSI DRIVER ***************")
			log.Info("**********************************************")

			log.Infof(">>>>> CMDLINE Exec, args: %v", args)
			defer log.Info("<<<<< CMDLINE Exec")

			if err := csiCliHandler(cmd); err != nil {
				log.Errorf("Failed to execute CLI handler, Err: %v", err.Error())
				os.Exit(1)
			}
		},
	}
)

// Initialize cmd-line flags/commands
func init() {
	RootCmd.PersistentFlags().StringVarP(&name, "name", "n", csiDriverName, "CSI driver name")
	RootCmd.PersistentFlags().StringVarP(&endpoint, "endpoint", "e", csiEndpoint, "CSI endpoint")
	RootCmd.PersistentFlags().StringVarP(&dbServer, "dbserver", "s", "", "Database server for the CSI driver")
	RootCmd.PersistentFlags().StringVarP(&dbPort, "dbport", "p", etcd.DefaultPort, "Database server port for the CSI driver")
	RootCmd.PersistentFlags().BoolP("node-service", "", false, "CSI node-plugin")
	RootCmd.PersistentFlags().BoolP("node-init", "", false, "CSI node-plugin")
	RootCmd.PersistentFlags().BoolP("node-monitor", "", false, "Enable monitoring the node")
	RootCmd.PersistentFlags().StringVarP(&nodeMonitorInterval, "node-monitor-interval", "", "30", "Interval in seconds to monitor the node")
	RootCmd.PersistentFlags().BoolP("help", "h", false, "Show help information")
	RootCmd.PersistentFlags().StringVarP(&flavorName, "flavor", "f", "", "CSI driver flavor")
	RootCmd.PersistentFlags().BoolP("pod-monitor", "", false, "Enable monitoring of pod statuses on unreachable nodes")
	RootCmd.PersistentFlags().StringVarP(&podMonitorInterval, "pod-monitor-interval", "", "30", "Interval in seconds to monitor pods")

}

//nolint:govet //TODO: fix the non-constant format string being sent to log.Errorf
func csiCliHandler(cmd *cobra.Command) error {
	log.Trace(">>>>> csiCliHandler")
	defer log.Trace("<<<<< csiCliHandler")

	// Process cmd-line arguments for the CSI driver
	driverName, _ := cmd.Flags().GetString("name")
	nodeService, _ := cmd.Flags().GetBool("node-service")
	nodeInit, _ := cmd.Flags().GetBool("node-init")
	endpoint, _ := cmd.Flags().GetString("endpoint")
	dbServer, _ := cmd.Flags().GetString("dbserver")
	dbPort, _ := cmd.Flags().GetString("dbport")
	flavorName, _ := cmd.Flags().GetString("flavor")
	podMonitor, _ := cmd.Flags().GetBool("pod-monitor")
	podMonitorInterval, _ := cmd.Flags().GetString("pod-monitor-interval")
	nodeMonitor, _ := cmd.Flags().GetBool("node-monitor")
	nodeMonitorInterval, _ := cmd.Flags().GetString("node-monitor-interval")

	// Parse the endpoint
	_, addr, err := driver.ParseEndpoint(endpoint)
	if err != nil {
		log.Errorf("%s", err.Error())
		return err
	}

	// Check if the endpoint's dirpath exists
	dirPath := filepath.Dir(addr)
	exists, isDir, err := util.FileExists(dirPath)
	if err != nil {
		return fmt.Errorf("Error while processing the filepath %v, err: %v", dirPath, err.Error())
	}
	if !exists || !isDir {
		return fmt.Errorf("Directory path %v does not exist", dirPath)
	}

	// Set the flavor
	if flavorName == "" {
		flavorName = flavor.Vanilla
	}

	if nodeInit {
		// Configure host block storage services (iscsid/multipathd) unless disabled.
		configureNodeServices()

		nodeInitContainer := nodeinit.NewNodeInitContainer(flavorName)
		err := nodeInitContainer.NodeInit()
		if err != nil {
			log.Errorf("Error while initiating the hpe csi driver container, refer the logs for more information: %s", err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}

	monitorInterval, err := strconv.ParseInt(podMonitorInterval, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid interval %s provided for monitoring pods on unreachable nodes", podMonitorInterval)
	}

	var nmInterval int64
	disableNodeMonitor := os.Getenv("DISABLE_NODE_MONITOR")
	if disableNodeMonitor == "true" {
		log.Infof("Node monitor is disabled, DISABLE_NODE_MONITOR=%v."+
			"Skipping the node monitor", disableNodeMonitor)
		nodeMonitor = false
	} else {
		nmInterval, err = strconv.ParseInt(nodeMonitorInterval, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid interval %d provided for monitoring the node", nmInterval)
		}
	}

	pid := os.Getpid()
	d, err := driver.NewDriver(
		driverName,
		csiVersion,
		endpoint,
		flavorName,
		nodeService,
		dbServer,
		dbPort,
		podMonitor,
		monitorInterval,
		nodeMonitor,
		nmInterval)
	if err != nil {
		return fmt.Errorf("Error instantiating plugin %v, Err: %v", driverName, err.Error())
	}
	log.Infof("About to start the CSI driver '%v with KubeletRootDirectory %s'", driverName, d.KubeletRootDir)

	d.Start(nodeService)
	log.Infof("[%d] reply  : %v", pid, os.Args)
	chanDone := d.StartScrubber(nodeService) // Start scrubber task

	// Handle signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt,
		syscall.SIGSEGV,
		syscall.SIGTERM)

	s := <-stop
	log.Infof("Exiting due to signal [%v] notification for pid [%d]", s.String(), pid)
	d.StopScrubber(nodeService, chanDone) // Stop scrubber task
	d.Stop(nodeService)
	log.Infof("Stopped [%d]", pid)
	return nil
}

// Environment variables and host paths used by configureNodeServices.
const (
	envDisableNodeConfiguration = "DISABLE_NODE_CONFIGURATION"
	envDisableNodeISCSIConfig   = "DISABLE_NODE_ISCSI_CONFIGURATION"
	envForceNodeISCSIConfig     = "FORCE_NODE_ISCSI_CONFIGURATION"
	envNodeServiceConfigFatal   = "NODE_SERVICE_CONFIGURATION_FATAL"

	// Host paths (the node containers bind-mount the host root at /host) that
	// show whether iSCSI is configured or in use on this node.
	hostISCSIInitiatorNameFile = "/host/etc/iscsi/initiatorname.iscsi"
	hostISCSISessionSysfsDir   = "/sys/class/iscsi_session"
	hostISCSIHostSysfsDir      = "/sys/class/iscsi_host"
	hostISCSINodesDBDir        = "/host/var/lib/iscsi/nodes"

	// envTrue is the value that turns the boolean environment switches on.
	envTrue = "true"
)

// envEnabled reports whether the given boolean environment switch is set to "true".
func envEnabled(name string) bool { return os.Getenv(name) == envTrue }

// iscsiInUseProbePaths lists the host paths whose presence (non-empty directory
// or existing file) means this node uses, or has been set up for, iSCSI.
var iscsiInUseProbePaths = []string{
	hostISCSISessionSysfsDir,
	hostISCSIHostSysfsDir,
	hostISCSINodesDBDir,
	hostISCSIInitiatorNameFile,
}

// shouldConfigureISCSI implements parent fix D2 of ESC-18251 (fix verification
// finding F7 / criterion A3): do not touch iscsid on nodes that do not use iSCSI.
// Every `systemctl enable/start` is a D-Bus round trip to PID 1 that can wait a
// long time on a dense node, and on the FC-only verification testbed the init
// called `systemctl start iscsid.service` on every restart for nothing.
//
// Decision order:
//   - DISABLE_NODE_ISCSI_CONFIGURATION=true -> never (operator opt-out)
//   - FORCE_NODE_ISCSI_CONFIGURATION=true   -> always (operator override for
//     first-boot nodes that have no sessions or initiator name yet)
//   - otherwise only if the host shows iSCSI in use or configured: an iSCSI
//     session or HBA in sysfs, a populated open-iscsi node database, or an
//     initiator name file.
func shouldConfigureISCSI(probePaths []string) (bool, string) {
	if envEnabled(envDisableNodeISCSIConfig) {
		return false, envDisableNodeISCSIConfig + "=true"
	}
	if envEnabled(envForceNodeISCSIConfig) {
		return true, envForceNodeISCSIConfig + "=true"
	}
	for _, p := range probePaths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !fi.IsDir() {
			return true, p + " exists"
		}
		if entries, err := os.ReadDir(p); err == nil && len(entries) > 0 {
			return true, p + " is not empty"
		}
	}
	return false, "no iSCSI session, HBA, node database or initiator name found on the host"
}

// configureNodeServices configures the host's iscsid and multipathd services
// from the node-init container.
//
// ESC-18251: these steps talk to the host's PID 1 over D-Bus (`systemctl
// enable`, `systemctl start`). On a densely loaded node a daemon-reload can
// take a minute or more and the systemctl client may time out even though
// PID 1 eventually completes the request. The services are typically already
// enabled and running, so a failure here is reported as a warning and the
// init container continues. Exiting would leave the node plugin in
// Init:CrashLoopBackOff and, with every retry requesting another full reload,
// keep PID 1 busy for the whole node.
//
// The warning names the real cause (fix verification N3): a refused bus
// connection means the DaemonSet runs without hostPID, a "Running in chroot"
// no-op means SYSTEMD_IGNORE_CHROOT is missing, and only a timeout means PID 1
// is busy. The same line also appears in /var/log/hpe-csi-node.log, so "host
// services unmanaged" is visible without the init container's log.
//
// Controls:
//   - DISABLE_NODE_CONFIGURATION=true skips everything (documented workaround).
//   - DISABLE_NODE_ISCSI_CONFIGURATION / FORCE_NODE_ISCSI_CONFIGURATION
//     override the iSCSI auto-detection (see shouldConfigureISCSI).
//   - NODE_SERVICE_CONFIGURATION_FATAL=true restores the previous fail-fast
//     behaviour for environments that want the init container to abort. It
//     covers only this Go step; the entrypoint's conformance unit install stays
//     best-effort.
func configureNodeServices() {
	if envEnabled(envDisableNodeConfiguration) {
		log.Infof("Skipping node configuration, %s=true. All block storage services needs to be installed and configured manually.", envDisableNodeConfiguration)
		return
	}

	fatal := envEnabled(envNodeServiceConfigFatal)
	handle := func(service string, err error) {
		if err == nil {
			return
		}
		kind := linux.ClassifySystemctlError(err)
		if fatal {
			log.Errorf("Unable to configure %s service: %s. err: %v", service, kind, err.Error())
			os.Exit(1)
		}
		log.Warnf("Host systemd step SKIPPED for the %s service: %s. The node may come up with %s unmanaged by the driver; "+
			"verify it on the host. Set %s=true to abort the init container instead. err: %v",
			service, kind, service, envNodeServiceConfigFatal, err.Error())
	}

	if configure, reason := shouldConfigureISCSI(iscsiInUseProbePaths); configure {
		log.Infof("Configuring iscsid (%s)", reason)
		handle("iscsid", tunelinux.ConfigureIscsi())
	} else {
		log.Infof("Skipping iscsid configuration (%s). Set %s=true to override.", reason, envForceNodeISCSIConfig)
	}

	handle("multipathd", tunelinux.ConfigureMultipath())
}

// Main runs csi interpreting command-line flags and commands
func Main() {

	if err := RootCmd.Execute(); err != nil {
		log.Error("Failed to execute, err:", err.Error())
		os.Exit(1)
	}
}

func main() {
	/* Start CSI plugin service */
	Main()
}
