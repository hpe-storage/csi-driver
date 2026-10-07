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
#   hpe-logcollector.sh.  Log files for the HPE CSI controller sidecar
#   containers and the CSP pods (which now log to an emptyDir, not host
#   /var/log) are also collected from kubectl logs into local files.
#

# Finds a node that matches the $node_name and updates $node_name
# value to the name from kubectl.  If the node is not found, the
# return value is non-zero.
sanitize_node_name() {
if [[ ! -z $node_name ]]; then
	node_from_kubectl=$(kubectl get nodes --field-selector=metadata.name=$node_name -o jsonpath='{.items..metadata.name}')
	if [[ -z $node_from_kubectl ]]; then
		return 1
	fi
	node_name=$node_from_kubectl
fi
}

# Runs the in-pod collector on one hpe-csi-node pod and, unless local copy is
# disabled, streams the resulting tarball back to the local dest_dir.
collect_from_pod() {
local pod_name=$1
local pod_node
pod_node=$(kubectl get pod "$pod_name" -n "$namespace" -o jsonpath='{.spec.nodeName}' 2>/dev/null)
echo "Collecting diagnostics from pod $pod_name (node ${pod_node:-unknown}) ..."

# No TTY (-t): a pseudo-terminal injects CRs and merges streams, which corrupts
# both the path parsing below and the streamed tarball bytes.
local output
if ! output=$(kubectl exec $pod_name -c hpe-csi-driver -n $namespace -- hpe-logcollector.sh 2>&1); then
	echo "  Failed to run hpe-logcollector.sh in pod $pod_name" >&2
	echo "$output" | sed 's/^/    /' >&2
	overall_rc=1
	return 1
fi
echo "$output" | sed 's/^/    /'

if [[ "$local_copy" != "true" ]]; then
	return 0
fi

# Prefer the exact archive this run created; fall back to the newest match.
local remote_path
remote_path=$(echo "$output" | sed -n 's/^Diagnostic dump file created at \(.*\) on host .*/\1/p' | tail -1)
if [[ -z "$remote_path" ]]; then
	remote_path=$(kubectl exec $pod_name -c hpe-csi-driver -n $namespace -- \
		/bin/sh -c 'ls -t /var/log/hpe-storage-logs-*.tar.gz 2>/dev/null | head -1' 2>/dev/null | tr -d '\r')
fi
if [[ -z "$remote_path" ]]; then
	echo "  No diagnostic archive found in pod $pod_name" >&2
	overall_rc=1
	return 1
fi

local base local_path
base=$(basename "$remote_path")
local_path="$dest_dir/$base"
echo "  -> $remote_path -> saving as $local_path"
if ! kubectl exec $pod_name -c hpe-csi-driver -n $namespace -- cat "$remote_path" > "$local_path" 2>/dev/null; then
	echo "  Failed to copy $remote_path from pod $pod_name" >&2
	rm -f "$local_path"
	overall_rc=1
	return 1
fi
if [[ ! -s "$local_path" ]]; then
	echo "  Copied archive $local_path is empty" >&2
	rm -f "$local_path"
	overall_rc=1
	return 1
fi

if [[ "$remove_remote" == "true" ]]; then
	kubectl exec $pod_name -c hpe-csi-driver -n $namespace -- rm -f "$remote_path" >/dev/null 2>&1
fi
return 0
}

diagnostic_collection() {
local pod_list
if [[ ! -z $node_name ]]; then
	pod_list=$(kubectl get pods -n $namespace --selector=app=hpe-csi-node --field-selector=spec.nodeName=$node_name -o jsonpath='{.items..metadata.name}')
	if [[ -z "$pod_list" ]]; then
		echo "Pod hpe-csi-node in namespace $namespace is not running on node $node_name."
		overall_rc=1
		return
	fi
else
	# collect the diagnostic logs from all the nodes where the hpe-csi-node pod is running
	pod_list=$(kubectl get pods -n $namespace --selector=app=hpe-csi-node -o jsonpath='{.items..metadata.name}')
fi

for pod_name in $pod_list
do
	collect_from_pod "$pod_name"
done
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
	dest_log_dir="$dest_dir"
	# Stage under the destination dir so no privileged /var/log write is needed.
	tmp_log_dir="$dest_dir/.hpe-csi-controller-logs-$timestamp"
	hostname=$(cat /etc/hostname 2>/dev/null || hostname)
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

# Collects CSP container logs via kubectl logs. The CSP logs to an emptyDir that
# is wiped when its pod is replaced, so --previous is also captured to recover the
# last terminated container's logs after a crash or restart (CON-2043, CON-3303).
csp_log_collection() {
node_selector=""
if [[ ! -z $node_name ]]; then
	node_selector="--field-selector=spec.nodeName=$node_name"
fi

# CSP pods all use the hpe-csp-sa service account, regardless of their app label.
pod_list=$(kubectl get pods -n $namespace $node_selector \
	-o jsonpath='{range .items[?(@.spec.serviceAccountName=="hpe-csp-sa")]}{.metadata.name}{" "}{end}')
if [[ ! -z "$pod_list" ]]
then
	timestamp=`date '+%Y%m%d_%H%M%S'`
	dest_log_dir="$dest_dir"
	# Stage under the destination dir so no privileged /var/log write is needed.
	tmp_log_dir="$dest_dir/.hpe-csp-logs-$timestamp"
	hostname=$(cat /etc/hostname 2>/dev/null || hostname)
	tar_file_name="hpe-csp-logs-$hostname-$timestamp.tar.gz"

	mkdir -p $tmp_log_dir

	for pod_name in $pod_list
	do
		container_list=$(kubectl get pod $pod_name -n $namespace -o jsonpath='{.spec.containers[*].name}')
		for container_name in $container_list
		do
			timeout 30 kubectl logs $pod_name -n $namespace -c $container_name &> $tmp_log_dir/$pod_name.$container_name.log
			# Previous instance exists only after a restart; drop the file if there is none.
			if timeout 30 kubectl logs --previous $pod_name -n $namespace -c $container_name > $tmp_log_dir/$pod_name.$container_name.previous.log 2>/dev/null; then
				[[ -s $tmp_log_dir/$pod_name.$container_name.previous.log ]] || rm -f $tmp_log_dir/$pod_name.$container_name.previous.log
			else
				rm -f $tmp_log_dir/$pod_name.$container_name.previous.log
			fi
		done
	done

	if [[ ! -z $(ls $tmp_log_dir) ]]
	then
		tar -czf $tar_file_name -C $tmp_log_dir . &> /dev/null
		mv $tar_file_name $dest_log_dir &> /dev/null
	fi

	rm -rf $tmp_log_dir

	if [[ -f "$dest_log_dir/$tar_file_name" ]]
	then
		echo "HPE CSP logs were collected into $dest_log_dir/$tar_file_name on host $hostname."
	else
		echo "Unable to collect HPE CSP log files."
	fi
fi
}

display_usage() {
echo "Collect HPE storage diagnostic logs using kubectl."
echo -e "\nUsage:"
echo -e "     hpe-logcollector.sh [-h|--help] [--node-name NODE_NAME] \\"
echo -e "                         [-n|--namespace NAMESPACE] [-a|--all] \\"
echo -e "                         [-d|--dest-dir DIR] [--no-local-copy] [--remove-remote]"
echo -e "Options:"
echo -e "-h|--help                  Print this usage text"
echo -e "--node-name NODE_NAME      Collect logs only for Kubernetes node NODE_NAME"
echo -e "-n|--namespace NAMESPACE   Collect logs from HPE CSI deployment in namespace"
echo -e "                           NAMESPACE (default: kube-system)"
echo -e "-a|--all                   Collect logs from all nodes (the default)"
echo -e "-d|--dest-dir DIR          Directory on this machine to save collected logs into"
echo -e "                           (default: ./hpe-storage-logs-<timestamp>)"
echo -e "--no-local-copy            Leave node tarballs on the nodes (legacy behavior);"
echo -e "                           do not copy them back to this machine"
echo -e "--remove-remote            Delete each node tarball after it is copied back\n"
exit 0

}
namespace="kube-system"
node_name=""
dest_dir=""
local_copy=true
remove_remote=false
overall_rc=0
#Main Function
if ! options=$(getopt -o han:d: -l help,all,namespace:,node-name:,dest-dir:,no-local-copy,remove-remote -- "$@")
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
    -d|--dest-dir) dest_dir=$2; shift;;
    --no-local-copy) local_copy=false;;
    --remove-remote) remove_remote=true;;
    -a|--all) ;;
    --) ;;
    *) echo "$0: unexpected parameter $1" >&2; exit 1;;
    esac
    shift
done

if ! sanitize_node_name; then
	echo "Node $node_name was not found."
	exit 1
fi

if [[ -z "$dest_dir" ]]; then
	dest_dir="./hpe-storage-logs-$(date '+%Y%m%d_%H%M%S')"
fi
if ! mkdir -p "$dest_dir" 2>/dev/null; then
	echo "Unable to create destination directory: $dest_dir" >&2
	exit 1
fi
dest_dir=$(cd "$dest_dir" && pwd)

diagnostic_collection
controller_log_collection
csp_log_collection

if [[ "$local_copy" == "true" ]]; then
	echo "All HPE storage logs collected locally in: $dest_dir"
fi
exit $overall_rc
