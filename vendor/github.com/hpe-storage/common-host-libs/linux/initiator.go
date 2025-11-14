// Copyright 2019 Hewlett Packard Enterprise Development LP

package linux

import (
	"errors"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/model"
	"github.com/hpe-storage/common-host-libs/util"
)

var (
	initiatorPath        = "/etc/iscsi/initiatorname.iscsi"
	initiatorNamePattern = "^InitiatorName=(?P<iscsiinit>.*)$"
	iscsi                = "iscsi"
	fc                   = "fc"
	nvmeHostnqnPath      = "/etc/nvme/hostnqn"        
    	nvmeHostnqnPattern   = "^(?P<nvmeinit>nqn\\..*)$"
	nvmeotcp             = "nvmeotcp"
)

//GetInitiators : get the host initiators
func GetInitiators() ([]*model.Initiator, error) {
	log.Trace(">>>>> GetInitiators")
	defer log.Trace("<<<<< GetInitiators")

	//var inits Initiators
	var inits []*model.Initiator
	iscsiInits, err := getIscsiInitiators()
	if err != nil {
		log.Debug("Error getting iscsiInitiator: ", err)
	}

	fcInits, err := getFcInitiators()
	if err != nil {
		log.Debug("Error getting FcInitiator: ", err)
	}
	// Add NVMe initiator discovery
	nvmeInits, err := getNvmeInitiators()
	if err != nil {
		log.Debug("Error getting NvmeInitiator: ", err)
	}

	if fcInits != nil {
		inits = append(inits, fcInits)
	}
	if iscsiInits != nil {
		inits = append(inits, iscsiInits)
	}
	if nvmeInits != nil {
		inits = append(inits, nvmeInits)
	}

	if fcInits == nil && iscsiInits == nil && nvmeInits == nil {
		return nil, errors.New("iscsi, fc, and nvme initiators not found")
	}

	log.Debug("initiators ", inits)
	return inits, err
}

func getIscsiInitiators() (init *model.Initiator, err error) {
	log.Trace(">>>>> getIscsiInitiator")
	defer log.Trace("<<<<< getIscsiInitiators")

	exists, _, err := util.FileExists(initiatorPath)
	if !exists {
		log.Debugf("%s not found, assuming not an iscsi host", initiatorPath)
		return nil, nil
	}
	initiators, err := util.FileGetStringsWithPattern(initiatorPath, initiatorNamePattern)
	if err != nil {
		log.Errorf("failed to get iqn from %s error %s", initiatorPath, err.Error())
		return nil, err
	}
	if len(initiators) == 0 {
		log.Errorf("empty iqn found from %s", initiatorPath)
		return nil, errors.New("empty iqn found")
	}
	log.Debugf("got iscsi initiator name as %s", initiators[0])
	// fetch CHAP credentials
	chapInfo, err := GetChapInfo()
	if err != nil {
		return nil, err
	}
	init = &model.Initiator{Type: iscsi, Init: initiators, Chap: chapInfo}
	return init, err
}

// GetFcInitiators get all host fc initiators (port WWNs)
func getFcInitiators() (fcInit *model.Initiator, err error) {
	log.Trace(">>>>> getFcInitiators")
	defer log.Trace("<<<<< getFcInitiators")

	inits, err := GetAllFcHostPortWWN()
	if err != nil {
		return nil, err
	}
	if len(inits) == 0 {
		// not a fc host
		return nil, nil
	}
	fcInit = &model.Initiator{
		Type: fc,
		Init: inits,
	}
	return fcInit, nil
}

/*func getNvmeInitiators() (init *model.Initiator, err error) {
	log.Trace(">>>>> getNvmeInitiators")
	defer log.Trace("<<<<< getNvmeInitiators")

	hostnqn, err := GetNvmeInitiator()
	if err != nil {
		log.Debugf("NVMe host NQN not found, assuming not an NVMe host")
		return nil, nil
	}

	initiators := []string{hostnqn}
	init = &model.Initiator{Type: "nvmeotcp", Init: initiators}
	return init, nil
}*/
func getNvmeInitiators() (init *model.Initiator, err error) {
    log.Trace(">>>>> getNvmeInitiators")
    defer log.Trace("<<<<< getNvmeInitiators")

    exists, _, err := util.FileExists(nvmeHostnqnPath)
    if !exists {
        log.Debugf("%s not found, assuming not an NVMe host", nvmeHostnqnPath)
        return nil, nil
    }
    
    initiators, err := util.FileGetStringsWithPattern(nvmeHostnqnPath, nvmeHostnqnPattern)
    if err != nil {
        log.Errorf("failed to get nqn from %s error %s", nvmeHostnqnPath, err.Error())
        return nil, err
    }
    
    if len(initiators) == 0 {
        log.Errorf("empty nqn found from %s", nvmeHostnqnPath)
        return nil, errors.New("empty nqn found")
    }
    
    log.Debugf("got nvme initiator name as %s", initiators[0])
    init = &model.Initiator{Type: nvmeotcp, Init: initiators}
    return init, nil
}
