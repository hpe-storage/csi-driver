/*
(c) Copyright 2019 Hewlett Packard Enterprise Development LP

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

package connectivity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hpe-storage/common-host-libs/jsonutil"
	log "github.com/hpe-storage/common-host-libs/logger"
)

const (
	defaultTimeout = time.Duration(60) * time.Second
)

//Request encapsulates a request to the Do* family of functions
type Request struct {
	//Action to take, ie: GET, POST, PUT, PATCH, DELETE
	Action string
	//Path is the URI
	Path string
	//Header to include one or more headers in the request
	Header map[string]string
	//Payload to send (may be nil)
	Payload interface{}
	//Response to marshal into (may be nil)
	Response interface{}
	//ResponseError to marshal error into (may be nil)
	ResponseError interface{}
}

// Client is a simple wrapper for http.Client
type Client struct {
	*http.Client
	pathPrefix string
}

// NewHTTPClient returns a client that communicates over ip using a 30 second timeout
func NewHTTPClient(url string) *Client {
	return NewHTTPClientWithTimeout(url, defaultTimeout)
}

// NewHTTPClientWithTimeout returns a client that communicates over ip
func NewHTTPClientWithTimeout(url string, timeout time.Duration) *Client {
	if timeout < 1 {
		timeout = defaultTimeout
	}
	return &Client{&http.Client{Timeout: timeout}, url}
}

// NewHTTPClientWithTimeoutAndRedirectPolicy returns a client that communicates over ip.
// The redirectPoliyFunc to handle the redirect behaviors on the headers before forwarding.
func NewHTTPClientWithTimeoutAndRedirectPolicy(url string, timeout time.Duration, redirectPolicyFunc func(*http.Request, []*http.Request) error) *Client {
	if timeout < 1 {
		timeout = defaultTimeout
	}
	return &Client{&http.Client{Timeout: timeout, CheckRedirect: redirectPolicyFunc}, url}
}

// NewHTTPSClientWithTimeout returns a client that communicates over ip with tls :
func NewHTTPSClientWithTimeout(url string, transport http.RoundTripper, timeout time.Duration) *Client {
	if timeout < 1 {
		timeout = defaultTimeout
	}
	return &Client{&http.Client{Timeout: timeout, Transport: transport}, url}
}

// NewHTTPSClientWithTimeoutAndRedirectPolicy returns a client that communicates over ip
func NewHTTPSClientWithTimeoutAndRedirectPolicy(url string, transport http.RoundTripper, timeout time.Duration, redirectPolicyFunc func(*http.Request, []*http.Request) error) *Client {
	if timeout < 1 {
		timeout = defaultTimeout
	}
	return &Client{&http.Client{Timeout: timeout, Transport: transport, CheckRedirect: redirectPolicyFunc}, url}
}

// NewHTTPSClient returns a new https client
func NewHTTPSClient(url string, transport http.RoundTripper) *Client {
	return NewHTTPSClientWithTimeout(url, transport, defaultTimeout)
}

// NewSocketClient returns a client that communicates over a unix socket using a 30 second connect timeout
func NewSocketClient(filename string) *Client {
	return NewSocketClientWithTimeout(filename, defaultTimeout)
}

// NewSocketClientWithTimeout returns a client that communicates over a unix file socket
func NewSocketClientWithTimeout(filename string, timeout time.Duration) *Client {
	if timeout < 1 {
		timeout = defaultTimeout
	}
	tr := &http.Transport{
		DisableCompression: true,
	}
	tr.Dial = func(_, _ string) (net.Conn, error) {
		return net.DialTimeout("unix", filename, timeout)
	}
	return &Client{&http.Client{Transport: tr, Timeout: timeout}, "http://unix"}
}

// Helper function to check if the error response is parsable for the given status code
func isParsableError(statusCode int) bool {
	switch statusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout: /* 502, 503, 504 */
		log.Infof("Received non-parsable error code: %v", statusCode)
		return false
	default:
		return true
	}
}

// DoJSON action on path.  payload and response are expected to be structs that decode/encode from/to json
// Example action=POST, path=/VolumeDriver.Create ...
// Tries 3 times to get data from the server
// nolint : To avoid cyclomatic complexity error
func (client *Client) DoJSON(r *Request) (int, error) {
	// make sure we have a root slash
	if !strings.HasPrefix(r.Path, "/") {
		r.Path = client.pathPrefix + "/" + r.Path
	} else {
		r.Path = client.pathPrefix + r.Path
	}

	var buf bytes.Buffer
	// encode the payload
	if r.Payload != nil {
		if err := json.NewEncoder(&buf).Encode(r.Payload); err != nil {
			return 0, err
		}
	}

	// build request
	req, err := http.NewRequest(r.Action, r.Path, &buf)
	if err != nil {
		return 0, err
	}

	// Add headers
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	// Include other headers specified in the input request
	for key, val := range r.Header {
		req.Header.Add(key, val)
		if log.IsSensitive(key) {
			// Log sensitive info as *****
			log.Tracef("Header: {%v : %v}\n", key, "*****")
		} else {
			log.Tracef("Header: {%v : %v}\n", key, val)
		}
	}

	req.Close = true
	log.Tracef("Request: action=%s path=%s", r.Action, r.Path)

	// execute the do
	res, err := doWithRetry(client, req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	// check the status code
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusNoContent {
		log.Errorf("status code was %s for request: action=%s path=%s, attempting to decode error response.", res.Status, r.Action, r.Path)
		// Check if this error is parsable
		if isParsableError(res.StatusCode) {
			// Decode the body into the error response
			err = decode(res.Body, r.ResponseError, r)
			if err != nil {
				log.Error("Failed to decode error response.")
				r.ResponseError = "Failed to decode error response, Error:" + fmt.Sprintf("%d", res.StatusCode)
				return res.StatusCode, err
			}
		}
		return res.StatusCode, fmt.Errorf("status code was %s for request: action=%s path=%s", res.Status, r.Action, r.Path)
	}

	// Docker /info always has contentLength =-1 so that is not the sufficient condition to not decode the body.
	// Rather check for io.EOF and do not throw error if empty body exist
	err = decode(res.Body, r.Response, r)
	if err != nil {
		return res.StatusCode, err
	}
	return res.StatusCode, nil
}

func doWithRetry(client *Client, request *http.Request) (*http.Response, error) {
	try := 0
	maxTries := 3
	for {
		response, err := client.Do(request)
		if err != nil {
			// return timeout as an error rather than retrying again as the caller has already hit timeout
			if strings.Contains(strings.ToLower(err.Error()), "timeout") {
				return nil, err
			}
			if try < maxTries {
				try++
				time.Sleep(time.Duration(try) * time.Second)
				continue
			}
			return nil, err
		}
		log.Tracef("response: %v, length=%v", response.Status, response.ContentLength)
		return response, nil
	}
}

func decode(rc io.ReadCloser, dest interface{}, r *Request) error {
	if rc != nil && dest != nil {
		log.Debugf("About to decode the error response %v into destination interface", rc)
		if err := json.NewDecoder(rc).Decode(&dest); err != nil {
			switch {
			case err == io.EOF:
				log.Tracef("empty body %s", io.EOF.Error())
				return nil
			}
			log.Errorf("unable to decode %v returned from action=%s to path=%s. error=%v", rc, r.Action, r.Path, err)
			jsonutil.PrintPrettyJSONToLog(rc)
			return err
		}
		return nil // Successfully decoded error response into destination interface.
	}
	if rc != nil {
		log.Debugf("Received a null reader. That is not expected.")
	}
	if dest != nil {
		log.Debugf("Received a null destination interface. That is not expected.")
	}
	return nil
}
