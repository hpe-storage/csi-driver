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
#   invoking hpe-logcollector.sh on each node using kubectl
#

diagnostic_collection() {
if [[ ! -z $node_name ]]; then
	# verify if this is a valid node name
	node_from_kubectl=$(kubectl get nodes -n $namespace --field-selector=metadata.name=$node_name -o json -o jsonpath='{.items..metadata.name}')
	if [[ -z $node_from_kubectl ]]; then
		echo "Please enter a valid node name/namespace"
		exit 0
	else
	  pod_name=$(kubectl get pods -n $namespace --selector=app=hpe-csi-node --field-selector=spec.nodeName=$node_from_kubectl -o json -o jsonpath='{.items..metadata.name}')
	  # hpe-csi-node pod may not be running on this node.
	  if [[ ! -z $pod_name ]]; then
	     kubectl exec -it $pod_name -c hpe-csi-driver -n $namespace -- hpe-logcollector.sh;
	  else
	     echo "hpe-csi-node pod is not running on node=$node_from_kubectl in namepspace=$namespace. Please specify valid node/namespace on which hpe-csi-node is running."
	  fi
	fi
else
	# collect the diagnostic logs from all the nodes
	for i in `kubectl get pods -n $namespace --selector=app=hpe-csi-node -o json -o jsonpath='{.items..metadata.name}'` ;
	do
		kubectl exec -it $i -c hpe-csi-driver -n $namespace -- hpe-logcollector.sh;
	done;
fi

}

display_usage() {
echo "Diagnostic Script to collect HPE Storage logs using kubectl"
echo -e "\nUsage:"
echo -e "     hpe-logcollector.sh [-h|--help][--node-name NODE_NAME][-n|--namespace NAMESPACE][-a|--all]"
echo -e "Where"
echo -e "-h|--help                  Print the Usage text"
echo -e "--node-name NODE_NAME      where NODE_NAME is kubernetes Node Name needed to collect the"
echo -e "                           hpe diagnostic logs of the Node"
echo -e "-n|--namespace NAMESPACE   where NAMESPACE is namespace of the pod deployment. default is kube-system"
echo -e "-a|--all                   collect diagnostic logs of all the nodes.If "
echo -e "                           nothing is specified logs would be collected"
echo -e "                           from all the nodes\n"
exit 0

}
namespace="kube-system"
node_name=""
#Main Function
if ! options=$(getopt -o han: -l help,all,namespace,node-name: -- "$@")
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
    -a|--all) shift;;
    (--) shift;;
    (-*) echo "$0: error - unrecognized option $1" 1>&2; exit 1;;
    (*) break;;
    esac
    shift
done
diagnostic_collection