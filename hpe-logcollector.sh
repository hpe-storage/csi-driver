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
#   This script collects log files and other diagnostics into a single
#   tar file for each specified node, using kubectl to invoke
#   hpe-logcollector.sh.  Log files for the HPE CSI controller
#   containers are also collected from kubectl logs into a local file.
#

# Finds a node that matches the $node_name value in $namespace and
# updates $node_name to the name from kubectl.  If the node is not
# found, the return value is non-zero.
sanitize_node_name() {
if [[ ! -z $node_name ]]; then
	node_from_kubectl=$(kubectl get nodes --field-selector=metadata.name=$node_name -o jsonpath='{.items..metadata.name}')
	if [[ -z $node_from_kubectl ]]; then
		return 1
	fi
	node_name=$node_from_kubectl
fi
}

diagnostic_collection() {
if [[ ! -z $node_name ]]; then
	pod_name=$(kubectl get pods -n $namespace --selector=app=hpe-csi-node --field-selector=spec.nodeName=$node_name -o jsonpath='{.items..metadata.name}')
	if [[ ! -z $pod_name ]]; then
	     kubectl exec -it $pod_name -c hpe-csi-driver -n $namespace -- hpe-logcollector.sh
	else
	     echo "hpe-csi-node pod in namespace $namespace is not running on node $node_name."
	fi
else
	# collect the diagnostic logs from all the nodes where the hpe-csi-node pod is running
	for i in $(kubectl get pods -n $namespace --selector=app=hpe-csi-node -o jsonpath='{.items..metadata.name}')
	do
		kubectl exec -it $i -c hpe-csi-driver -n $namespace -- hpe-logcollector.sh
	done
fi
}

# Collects CSI controller sidecar container logs if running on the specified node.
controller_log_collection() {
node_selector=""
if [[ ! -z $node_name ]]; then
	node_selector="--field-selector=spec.nodeName=$node_name"
fi

pod_list=$(kubectl get pods -n $namespace --selector=app=hpe-csi-controller $node_selector -o jsonpath='{.items..metadata.name}')
if [[ ! -z "$pod_list" ]]
then
	timestamp=`date '+%Y%m%d_%H%M%S'`
	tmp_log_dir="/var/log/hpe-csi-controller-logs-$timestamp"
	dest_log_dir="/var/log"
	hostname=$(cat /etc/hostname)
	tar_file_name="hpe-csi-controller-logs-$hostname-$timestamp.tar.gz"

	mkdir -p $tmp_log_dir

	for pod_name in $pod_list
	do
		container_list=$(kubectl get pod $pod_name -n $namespace -o jsonpath='{.spec.containers[*].name}')
		if [[ ! -z "$container_list" ]]
		then
			for container_name in $container_list
			do
				# The hpe-csi-driver log is collected in the hpe-csi-node dump
				if [[ "$container_name" != "hpe-csi-driver" ]]
				then
					timeout 30 kubectl logs $pod_name -n $namespace -c $container_name &> $tmp_log_dir/$pod_name.$container_name.log
				fi
			done
		fi
	done

	if [[ ! -z $(ls $tmp_log_dir) ]]
	then
		tar -czf $tar_file_name -C $tmp_log_dir . &> /dev/null
		mv $tar_file_name $dest_log_dir &> /dev/null
	fi

	rm -rf $tmp_log_dir

	if [[ -f "$dest_log_dir/$tar_file_name" ]]
	then
		echo "HPE CSI controller logs were collected into $dest_log_dir/$tar_file_name on host $hostname."
	else
		echo "Unable to collect HPE CSI controller log files."
	fi
fi
}

display_usage() {
echo "Collect HPE storage diagnostic logs using kubectl."
echo -e "\nUsage:"
echo -e "     hpe-logcollector.sh [-h|--help] [--node-name NODE_NAME] \\"
echo -e "                         [-n|--namespace NAMESPACE] [-a|--all]"
echo -e "Options:"
echo -e "-h|--help                  Print this usage text"
echo -e "--node-name NODE_NAME      Collect logs only for Kubernetes node NODE_NAME"
echo -e "-n|--namespace NAMESPACE   Collect logs from HPE CSI deployment in namespace"
echo -e "                           NAMESPACE (default: kube-system)"
echo -e "-a|--all                   Collect logs from all nodes (the default)\n"
exit 0

}
namespace="kube-system"
node_name=""
#Main Function
if ! options=$(getopt -o han: -l help,all,namespace:,node-name: -- "$@")
then
    exit 1
fi

eval set -- $options
while [ $# -gt 0 ]
do
key="$1"
    case $key in
    -h|--help) display_usage; break;;
    -n|--namespace) namespace=$2; shift;;
    --node-name) node_name=$2; shift;;
    -a|--all) ;;
    --) ;;
    *) echo "$0: unexpected parameter $1" >&2; exit 1;;
    esac
    shift
done

sanitize_node_name()
if [[ $? -ne 0 ]]; then
	echo "Node $node_name was not found."
	exit 1
fi
diagnostic_collection
controller_log_collection
