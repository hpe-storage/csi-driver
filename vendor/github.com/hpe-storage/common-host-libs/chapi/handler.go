// Copyright 2019 Hewlett Packard Enterprise Development LP

package chapi

// ErrorResponse struct
type ErrorResponse struct {
	Info string `json:"info,omitempty"`
}

//Response :
type Response struct {
	Data interface{} `json:"data,omitempty"`
	Err  interface{} `json:"errors,omitempty"`
}
