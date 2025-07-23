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
    echo $os_name | egrep -i "Red Hat|CentOS|Amazon Linux|Oracle|Rocky Linux|AlmaLinux" >> /dev/null 2>&1
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
    echo $os_name | egrep -i "SLES" >> /dev/null 2>&1
    if [ $? -eq 0 ]; then
        CONFORM_TO=sles
    fi
    echo $os_name | egrep -i "SLE Micro|SL-Micro" >> /dev/null 2>&1
    if [ $? -eq 0 ]; then
        CONFORM_TO=slem
    fi
fi

if [ ! -f /etc/multipath.conf ]; then
    nomultipathconf=true
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
    fi

    # Install nfs client packages
    if [ ! -f /sbin/mount.nfs4 ]; then
        apt-get -qq update
        apt-get -qq install -y nfs-common
        systemctl enable nfs-utils.service
        systemctl start nfs-utils.service
        exit_on_error $?
    fi

    # Install XFS utils
    if [ ! -f /sbin/mkfs.xfs ]; then
        apt-get -qq update
        apt-get -qq install -y xfsprogs
        exit_on_error $?
    fi

    # Install SG3 utils
    if [ ! -f /usr/bin/sg_inq ]; then
        apt-get -qq update
        apt-get -qq install -y sg3-utils
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
    fi

    # Install nfs client packages
    if [ ! -f /sbin/mount.nfs4 ]; then
        yum -y install nfs-utils
        systemctl enable nfs-utils.service
        systemctl start nfs-utils.service
        exit_on_error $?
    fi

    # Install SG3 utils package
    if [ ! -f /usr/bin/sg_inq ]; then
        yum -y install sg3_utils
        exit_on_error $?
    fi

elif [ "$CONFORM_TO" = "sles" ]; then
    # Install device-mapper-multipath
    if [ ! -f /sbin/multipathd ]; then
        zypper -n install multipath-tools
        exit_on_error $?
    fi

    # Install iscsi packages
    if [ ! -f /sbin/iscsid ]; then
        zypper -n install open-iscsi
        exit_on_error $?
    fi

    # Install nfs client packages
    if [ ! -f /sbin/mount.nfs4 ]; then
        zypper -n install nfs-client
        systemctl enable nfs-utils.service
        systemctl start nfs-utils.service
        exit_on_error $?
    fi

    # Install SG3 utils package
    if [ ! -f /usr/bin/sg_inq ]; then
        zypper -n install sg3_utils
        exit_on_error $?
    fi

elif [ "$CONFORM_TO" = "slem" ]; then
    # SLE Micro
    echo -n "Ensuring critical binaries are in the SLEM image: "
    for bin in /usr/bin/sg_inq /sbin/mount.nfs4 /sbin/iscsid /sbin/multipathd; do
        echo -n "$bin "
        if [ ! -f $bin ]; then
            echo "$bin is missing. Run 'transactional-update -n pkg install multipath-tools open-iscsi nfs-client sg3_utils' on the worker nodes and reboot."
            exit_on_error 1
        fi
    done

    systemctl start nfs-utils.service

elif [ "$CONFORM_TO" = "coreos" ]; then
    echo "skipping package checks/installation on CoreOS"
    if ! [[ -e /etc/iscsi/initiatorname.iscsi ]]; then
        echo "Generating first-boot IQN"
        echo "InitiatorName=$(iscsi-iname)" > /etc/iscsi/initiatorname.iscsi
    fi
else
    echo "unsupported configuration for node package checks. os $os_name"
    exit 1
fi

# Remove multipath.conf if installed during conformance with default packages
# Node driver will install correct multipath.conf
if [ "$nomultipathconf" = "true" ] && [ -f /etc/multipath.conf ]; then
	rm -f /etc/multipath.conf
fi

# Load iscsi_tcp modules, its a no-op if its already loaded
modprobe iscsi_tcp

# Don't let udev automatically scan targets(all luns) on Unit Attention.
# This will prevent udev scanning devices which we are attempting to remove
if [ -f /lib/udev/rules.d/90-scsi-ua.rules ]; then
    sed -i 's/^[^#]*scan-scsi-target/#&/' /lib/udev/rules.d/90-scsi-ua.rules
    udevadm control --reload-rules
fi
