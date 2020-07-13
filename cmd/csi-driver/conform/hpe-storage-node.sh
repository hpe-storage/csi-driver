#!/bin/bash

exit_on_error() {
    exit_code=$1
    if [ $exit_code -ne 0 ]; then
        echo "command failed with exit code ${exit_code}."
        exit $exit_code
    fi
}

# Obtain OS info
if [ -f /etc/os-release ]; then
    os_name=$(cat /etc/os-release | egrep "^NAME=" | awk -F"NAME=" '{print $2}')
    echo "os name obtained as $os_name"
    echo $os_name | egrep -i "Red Hat|CentOS" >> /dev/null 2>&1
    if [ $? -eq 0 ]; then
        CONFORM_TO=redhat
    fi
    echo $os_name | egrep -i "Ubuntu|Debian" >> /dev/null 2>&1
    if [ $? -eq 0 ]; then
        CONFORM_TO=ubuntu
    fi
    echo $os_name | egrep -i "CoreOS|Fedora" >> /dev/null 2>&1
    if [ $? -eq 0 ]; then
        CONFORM_TO=coreos
    fi
fi

if [ "$CONFORM_TO" = "ubuntu" ]; then
    #  Install multipath packages
    if [ ! -f /sbin/multipathd ]; then
        apt-get -qq update
        apt-get -qq install -y multipath-tools
        exit_on_error $?
    fi

    #  Install iscsi packages
    if [ ! -f /sbin/iscsid ]; then
        apt-get -qq update
        apt-get -qq install -y open-iscsi
        # exit with error to trigger restart of pod to mount newly installed iscisadm
        exit 1
    fi

    # Install nfs client packages
    if [ ! -f /sbin/mount.nfs4 ]; then
        apt-get -qq update
        apt-get -qq install -y nfs-common
        systemctl enable nfs-utils.service
        systemctl start nfs-utils.service
        exit_on_error $?
    fi

elif [ "$CONFORM_TO" = "redhat" ]; then
    # Install device-mapper-multipath
    if [ ! -f /sbin/multipathd ]; then
        yum -y install device-mapper-multipath
        exit_on_error $?
    fi

    # Install iscsi packages
    if [ ! -f /sbin/iscsid ]; then
        yum -y install iscsi-initiator-utils
        # exit with error to trigger restart of pod to mount newly installed iscisadm
        exit 1
    fi

    # Install nfs client packages
    if [ ! -f /sbin/mount.nfs4 ]; then
        yum -y install nfs-utils
        systemctl enable nfs-utils.service
        systemctl start nfs-utils.service
        exit_on_error $?
    fi
elif [ "$CONFORM_TO" = "coreos" ]; then
    echo "skipping package checks/installation on CoreOS"
else
    echo "unsupported configuration for node package checks. os $os_name"
    exit 1
fi

# Load iscsi_tcp modules, its a no-op if its already loaded
modprobe iscsi_tcp

# Don't let udev automatically scan targets(all luns) on Unit Attention.
# This will prevent udev scanning devices which we are attempting to remove
if [ -f /lib/udev/rules.d/90-scsi-ua.rules ]; then
    sed -i 's/^[^#]*scan-scsi-target/#&/' /lib/udev/rules.d/90-scsi-ua.rules
    udevadm control --reload-rules
fi