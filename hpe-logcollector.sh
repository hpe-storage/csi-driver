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
node_name=$1
if [[ ! -z $node_name ]]; then
	# verify if this is a valid node name
	node_from_kubectl=$(kubectl get nodes -n kube-system --field-selector=metadata.name=$node_name -o json -o jsonpath='{.items..metadata.name}')
	if [[ -z $node_from_kubectl ]]; then
		echo "Please enter a valid node name"
		exit 0
	else
	  pod_name=$(kubectl get pods -n kube-system --selector=app=hpe-csi-node --field-selector=spec.nodeName=$node_from_kubectl -o json -o jsonpath='{.items..metadata.name}')
	  # hpe-csi-node pod may not be running on this node.
	  if [[ ! -z $pod_name ]]; then
	     kubectl exec -it $pod_name -c hpe-csi-driver -n kube-system -- hpe-logcollector.sh;
	  else
	  	 echo "hpe-csi-node pod is not running on this node. Please specify a node on which hpe-csi-node is running."
	  fi
	fi
else
	# collect the diagnostic logs from all the nodes
	for i in `kubectl get pods -n kube-system --selector=app=hpe-csi-node -o json -o jsonpath='{.items..metadata.name}'` ;
	do
		kubectl exec -it $i -c hpe-csi-driver -n kube-system -- hpe-logcollector.sh;
	done;
fi

}

display_usage() {
echo "Diagnostic Script to collect HPE Storage logs using kubectl"
echo -e "\nUsage:"
echo -e "     hpe-logcollector.sh [-h|--help][-n|--node-name NODE_NAME][-a|--all]"
echo -e "Where"
echo -e "-h|--help                  Print the Usage text"
echo -e "-n|--node-name NODE_NAME   Kubernetes Node Name needed to collect the"
echo -e "                           hpe diagnostic logs of the Node"
echo -e "-a|--all                   collect diagnostic logs of all the nodes.If "
echo -e "                           nothing is specified logs would be collected"
echo -e "                           from all the nodes\n"


}

#Main Function
if ! options=$(getopt -o han: -l help,all,node-name: -- "$@")
then
    exit 1
fi

eval set -- $options
while [ $# -gt 0 ]
do
    case $1 in
    -h|--help) display_usage; break;;
    -n|--node-name)  diagnostic_collection $2; shift; break;;
    -a|--all) shift ; diagnostic_collection ""; break;;
    (--) shift; diagnostic_collection ""; break;;
    (-*) echo "$0: error - unrecognized option $1" 1>&2; exit 1;;
    (*) break;;
    esac
    shift
done