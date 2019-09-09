/*
(c) Copyright 2017 Hewlett Packard Enterprise Development LP

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"encoding/base64"
	log "github.com/hpe-storage/common-host-libs/logger"
	"regexp"
)

const (
	// Regex pattern to match b64 encoded string
	base64StringRegexPattern = "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"
)

var (
	base64StringMatcher = regexp.MustCompile(base64StringRegexPattern)
)

// DecodeBase64Credential to plain text if its b64 encoded, else return the data as-is.
func DecodeBase64Credential(b64data string) (string, error) {
	log.Trace(">>>>> DecodeBase64Credential")
	defer log.Trace("<<<<< DecodeBase64Credential")

	if base64StringMatcher.MatchString(b64data) { // Match base64 encoded format
		decBytes, err := base64.StdEncoding.DecodeString(b64data)
		if err != nil {
			log.Error("Failed to decode the base64 credential ", b64data)
			return "", err
		}
		return string(decBytes), nil
	}
	return b64data, nil
}
