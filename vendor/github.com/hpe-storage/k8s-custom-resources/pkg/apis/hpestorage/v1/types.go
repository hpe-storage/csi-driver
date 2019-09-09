// Copyright 2019 Hewlett Packard Enterprise Development LP

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HPENodeInfo is a struct that wraps a node onto which the HPE CSI node service has been deployed
type HPENodeInfo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              HPENodeInfoSpec `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HPENodeInfoList is a list of HPENodeInfos
type HPENodeInfoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []HPENodeInfo `json:"items"`
}

// HPENodeInfoSpec defines the properties listed on an HPENodeInfo
type HPENodeInfoSpec struct {
	UUID         string   `json:"uuid"`
	IQNs         []string `json:"iqns,omitempty"`
	Networks     []string `json:"networks,omitempty"`
	WWPNs        []string `json:"wwpns,omitempty"`
	ChapUser     string   `json:"chap_user,omitempty"`
	ChapPassword string   `json:"chap_password,omitempty"`
}
