// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"errors"
	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
)

// Udevadm udev manager
const Udevadm = "udevadm"

// UdevadmTrigger trigger udev rules
func UdevadmTrigger() (err error) {
	var args []string
	args = []string{"trigger"}
	_, _, err = util.ExecCommandOutput(Udevadm, args)
	if err != nil {
		log.Error("Unable to trigger udev rules for /etc/udev/rules.d/99-nimble-tune.rules ", err.Error())
		err = errors.New("Error: Unable to trigger udev rules for /etc/udev/rules.d/99-nimble-tune.rules, reason: " + err.Error())
	}
	return err
}

// UdevadmReloadRules trigger udev rules
func UdevadmReloadRules() (err error) {
	var args []string
	args = []string{"control", "--reload-rules"}
	_, _, err = util.ExecCommandOutput(Udevadm, args)
	if err != nil {
		log.Error("Unable to reload udev rules for /etc/udev/rules.d/99-nimble-tune.rules ", err.Error())
		err = errors.New("Error: Unable to reload udev rules for /etc/udev/rules.d/99-nimble-tune.rules, reason: " + err.Error())
	}
	return err
}
