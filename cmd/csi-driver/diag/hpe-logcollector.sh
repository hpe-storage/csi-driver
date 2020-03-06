#!/bin/bash

# (c) Copyright 2019 Hewlett Packard Enterprise Development LP

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

# http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# hpe-logcollector.sh
#   This file generates all the logs and debug information
#   and tars it to /var/log/ directory on the machine.
#

log_collection() {
# Initializing the file and directory
timestamp=`date '+%Y%m%d_%H%M%S'`
hostname=$(cat /etc/hostname)
filename="hpe-storage-logs-$hostname-$timestamp.tar.gz"

destinationDir="/var/log/tmp"
#Destination Hpe logs directory
mkdir -p $destinationDir
logDir="/var/log"
directory="$destinationDir/hpestorage-logs-$today"

# Writing to  destinationDir/
echo "Command :iscsiadm -m session -P 3" > $destinationDir/iscsiadm-m-P3
timeout 30 iscsiadm -m session -P 3 >> $destinationDir/iscsiadm-m-P3 2>&1
echo "Command :ps -aux " > $destinationDir/ps-aux
timeout 30 ps -aux >> $destinationDir/ps-aux 2>&1
echo "Command :mount"> $destinationDir/mounts
timeout 30 mount >> $destinationDir/mounts 2>&1
echo "Command :dmsetup table " > $destinationDir/dmsetup
timeout 30 dmsetup table >> $destinationDir/dmsetup 2>&1
if rpm -qa > /dev/null 2>&1; then
    echo "Command :rpm -qa | egrep 'multipath|iscsi'" > $destinationDir/rpm
    rpm -qa | egrep "multipath|iscsi" >> $destinationDir/rpm 2>&1
elif dpkg -l > /dev/null 2>&1; then
    echo "Command :dpkg -l | egrep 'multipath|iscsi'" > $destinationDir/dpkg
    dpkg -l | egrep "multipath|iscsi" >> $destinationDir/dpkg 2>&1
fi
echo "Command :multipath -ll " > $destinationDir/multipath
timeout 30 multipath -ll >> $destinationDir/multipath 2>&1
echo "Command :multipathd show paths format '%w %d %t %i %o %T %c  %r %R %m %s'" > $destinationDir/multipathd
timeout 30 multipathd show paths format "%w %d %t %i %o %T %c  %r %R %m %s" >> $destinationDir/multipathd 2>&1
echo "Command :multipathd show maps format '%w %d %n %S' ">> $destinationDir/multipathd
timeout 30 multipathd show maps format "%w %d %n %S" >> $destinationDir/multipathd 2>&1
echo "Command :ip addr " > $destinationDir/host-info
ip addr >> $destinationDir/host-info 2>&1
echo "Command :hostname " >> $destinationDir/host-info
hostname >> $destinationDir/host-info 2>&1
echo "Command :uname -a " >> $destinationDir/host-info
uname -a >> $destinationDir/host-info 2>&1
echo "Command :lsb_release -a" >> $destinationDir/host-info
lsb_release -a >> $destinationDir/host-info 2>&1
echo "Command :cat /etc/redhat-release" >> $destinationDir/host-info
cat /etc/redhat-release >> $destinationDir/host-info 2>&1
echo "Command : systemctl status hpe-storage-node" >> $destinationDir/hpe-storage-node
systemctl status hpe-storage-node >> $destinationDir/hpe-storage-node 2>&1

#Destination Tar directory
mkdir -p $directory

if [ -d $directory ]; then
    #HPE Logs
	cp -r /var/log/hpe-*.log $directory > /dev/null 2>&1

	#copy messages  for RHEL systems
	cp /var/log/messages* $directory > /dev/null 2>&1

	#copy syslog for Ubuntu systems
	cp /var/log/syslog* $directory > /dev/null 2>&1

	#Nimble Config files
	cp /etc/multipath.conf $directory
	cp /etc/iscsi/iscsid.conf $directory > /dev/null 2>&1


	#dmsetup table  output
	cp $destinationDir/dmsetup $directory

	#dmsetup table  output
	cp $destinationDir/mounts $directory

	#rpm/dpkg package output
	if [ -f $destinationDir/rpm ]; then
	   cp $destinationDir/rpm $directory
	elif [ -f $destinationDir/dpkg ]; then
	   cp $destinationDir/dpkg $directory
	fi

	#multipath   output
	cp $destinationDir/multipath $directory

	#multipathd   output
	cp $destinationDir/multipathd $directory

	#Iscsiadm logs
	cp $destinationDir/iscsiadm-m-P3 $directory

	#ps-aux output
	cp $destinationDir/ps-aux $directory

	#host info output
	cp $destinationDir/host-info $directory

	#tar the files
	tar -cvzf $filename -C $directory . &> /dev/null
	mv $filename $logDir/  &> /dev/null

	#Clean up after Tar
	rm -rf $directory
	rm -rf $destinationDir

	if [[ -f "$logDir/$filename" ]]; then
		echo "Diagnostic dump file created at $logDir/$filename on host $hostname"
	else
		echo "Unable to collect the diagnostic information under $logDir"
		exit 1
	fi
else
		echo "$directory not created , try again"
		exit 1
fi

}

display_usage() {
echo "Diagnostic LogCollector Script to collect HPE Storage logs"
echo -e "\nUsage: hpe-logcollector"
}

#Main Function
# check whether user had supplied -h or --help . If yes display usage
	if [[ ( $1 == "--help") ||  $1 == "-h" ]]
	then
		display_usage
		exit 0
	fi

echo "======================================"
echo "Collecting the Diagnostic Information"
echo "======================================"
log_collection $1
echo "Complete"
echo "===================================="
exit 0