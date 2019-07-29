#!/bin/bash

# Die on errors and scream to the journal
set -xe

#  Install multipath packages
if [[ ! -f /sbin/multipathd ]]; then
    apt-get -qq update
    apt-get -qq install -y multipath-tools
fi

#  Install iscsi packages
if [ ! -f /sbin/iscsid ]; then
    apt-get -qq update
    apt-get -qq install -y open-iscsi
fi

# load iscsi_tcp modules, its a no-op if its already loaded
modprobe iscsi_tcp
