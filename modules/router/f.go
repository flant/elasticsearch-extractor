// Copyright © 2020 Uzhinskiy Boris
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package router

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"bytes"
	"net"

	"time"
)

type esError struct {
	Error struct {
		RootCause []struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"root_cause"`
		Type   string `json:"type"`
		Reason string `json:"reason"`
	} `json:"error"`
	Status int `json:"status"`
}

func (rt *Router) netClientPrepare() {
	var netTransport = &http.Transport{
		Dial: (&net.Dialer{
			Timeout: time.Duration(rt.conf.App.TimeOut) * time.Second,
		}).Dial,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	rt.nc = &http.Client{
		Timeout:   time.Second * time.Duration(rt.conf.App.TimeOut),
		Transport: netTransport,
	}
}

func (rt *Router) doDel(url string) ([]byte, error) {

	actionRequest, _ := http.NewRequest("DELETE", url, nil)
	if rt.conf.Elastic.Username != "" {
		actionRequest.SetBasicAuth(rt.conf.Elastic.Username, rt.conf.Elastic.Password)
	}

	actionRequest.Header.Set("Content-Type", "application/json")
	actionRequest.Header.Set("Connection", "keep-alive")

	actionResult, err := rt.nc.Do(actionRequest)
	if actionResult != nil {
		defer actionResult.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	if actionResult.StatusCode != 200 {
		return nil, errors.New("Wrong response: " + actionResult.Status)
	}

	body, err := ioutil.ReadAll(actionResult.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (rt *Router) doGet(url string) ([]byte, error) {

	actionRequest, _ := http.NewRequest("GET", url, nil)
	if rt.conf.Elastic.Username != "" {
		actionRequest.SetBasicAuth(rt.conf.Elastic.Username, rt.conf.Elastic.Password)
	}

	actionRequest.Header.Set("Content-Type", "application/json")
	actionRequest.Header.Set("Connection", "keep-alive")

	actionResult, err := rt.nc.Do(actionRequest)
	if actionResult != nil {
		defer actionResult.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	if actionResult.StatusCode != 200 {
		return nil, errors.New("Wrong response: " + actionResult.Status)
	}

	body, err := ioutil.ReadAll(actionResult.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (rt *Router) doPost(url string, request map[string]interface{}) ([]byte, error) {
	toBackend, _ := json.Marshal(request)

	actionRequest, _ := http.NewRequest("POST", url, bytes.NewReader(toBackend))
	if rt.conf.Elastic.Username != "" {
		actionRequest.SetBasicAuth(rt.conf.Elastic.Username, rt.conf.Elastic.Password)
	}

	actionRequest.Header.Set("Content-Type", "application/json")
	actionRequest.Header.Set("Connection", "keep-alive")

	actionResult, err := rt.nc.Do(actionRequest)
	if actionResult != nil {
		defer actionResult.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(actionResult.Body)
	if err != nil {
		return nil, err
	}

	if actionResult.StatusCode != 200 {
		var e esError
		_ = json.Unmarshal(body, &e)
		return nil, errors.New(e.Error.Reason)
	}

	return body, nil
}
