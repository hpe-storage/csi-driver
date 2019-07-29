#!/bin/bash

# Die on errors and scream to the journal
set -xe

# Install device-mapper-multipath
if [[ ! -f /sbin/multipathd ]]; then
    yum -y install device-mapper-multipath
fi

# Install iscsi packages
if [ ! -f /sbin/iscsid ]; then
    yum -y install iscsi-initiator-utils
fi

# load iscsi_tcp modules, its a no-op if its already loaded
modprobe iscsi_tcp
