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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"bytes"
	"net"
	"regexp"
	"time"

	"github.com/uzhinskiy/lib.go/helpers"
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
	tlsClientConfig := createTLSConfig(rt.conf.Snapshot.CAcert, rt.conf.Snapshot.ClientCert,
		rt.conf.Snapshot.ClientKey, rt.conf.Snapshot.InsecureSkipVerify)
	var netTransport = &http.Transport{
		Dial: (&net.Dialer{
			Timeout: time.Duration(rt.conf.App.TimeOut) * time.Second,
		}).Dial,
		TLSClientConfig: tlsClientConfig,
	}

	rt.nc["Snapshot"] = &http.Client{
		Timeout:   time.Second * time.Duration(rt.conf.App.TimeOut),
		Transport: netTransport,
	}

	if rt.conf.Search.Host != "" {
		tlsClientConfig := createTLSConfig(rt.conf.Search.CAcert, rt.conf.Search.ClientCert,
			rt.conf.Search.ClientKey, rt.conf.Search.InsecureSkipVerify)
		var netTransport = &http.Transport{
			Dial: (&net.Dialer{
				Timeout: time.Duration(rt.conf.App.TimeOut) * time.Second,
			}).Dial,
			TLSClientConfig: tlsClientConfig,
		}

		rt.nc["Search"] = &http.Client{
			Timeout:   time.Second * time.Duration(rt.conf.App.TimeOut),
			Transport: netTransport,
		}

	}

}

func (rt *Router) doDel(url string, cluster string) ([]byte, error) {

	actionRequest, _ := http.NewRequest("DELETE", url, nil)
	actionRequest.Header.Set("Content-Type", "application/json")
	actionRequest.Header.Set("Connection", "keep-alive")
	if cluster == "Search" {
		if rt.conf.Search.Username != "" {
			actionRequest.SetBasicAuth(rt.conf.Search.Username, rt.conf.Search.Password)
		}
	} else {
		if rt.conf.Snapshot.Username != "" {
			actionRequest.SetBasicAuth(rt.conf.Snapshot.Username, rt.conf.Snapshot.Password)
		}
	}

	actionResult, err := rt.nc[cluster].Do(actionRequest)
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

func (rt *Router) doGet(url string, cluster string) ([]byte, error) {

	actionRequest, _ := http.NewRequest("GET", url, nil)
	actionRequest.Header.Set("Content-Type", "application/json")
	actionRequest.Header.Set("Connection", "keep-alive")

	if cluster == "Search" {
		if rt.conf.Search.Username != "" {
			actionRequest.SetBasicAuth(rt.conf.Search.Username, rt.conf.Search.Password)
		}
	} else {
		if rt.conf.Snapshot.Username != "" {
			actionRequest.SetBasicAuth(rt.conf.Snapshot.Username, rt.conf.Snapshot.Password)
		}
	}
	actionResult, err := rt.nc[cluster].Do(actionRequest)
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

func (rt *Router) doPost(url string, request map[string]interface{}, cluster string) ([]byte, error) {
	toBackend, _ := json.Marshal(request)

	actionRequest, _ := http.NewRequest("POST", url, bytes.NewReader(toBackend))

	actionRequest.Header.Set("Content-Type", "application/json")
	actionRequest.Header.Set("Connection", "keep-alive")

	if cluster == "Search" {
		if rt.conf.Search.Username != "" {
			actionRequest.SetBasicAuth(rt.conf.Search.Username, rt.conf.Search.Password)
		}

	} else {
		if rt.conf.Snapshot.Username != "" {
			actionRequest.SetBasicAuth(rt.conf.Snapshot.Username, rt.conf.Snapshot.Password)
		}
	}

	actionResult, err := rt.nc[cluster].Do(actionRequest)
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

func (rt *Router) getNodes() ([]singleNode, error) {

	var nresp []singleNode
	var na nodesArray

	//	rt.nodes.RLock()
	//	defer rt.nodes.RUnlock()

	response, err := rt.doGet(rt.conf.Snapshot.Host+"_cat/nodes?format=json&bytes=b&h=ip,name,dt,du,dup,d&s=name", "Snapshot")
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(response, &nresp)
	if err != nil {
		return nil, err
	}
	s := 0
	for i, n := range nresp {
		nresp[i].Dt = fmt.Sprintf("%dGb", helpers.Atoi(n.Dt)/(1024*1024*1024))
		na.list = append(na.list, helpers.Atoi(n.D))
		s += helpers.Atoi(n.D)
	}
	na.sum = s
	na.max = helpers.GetMaxValueInArray(na.list)
	rt.nodes = na
	return nresp, nil

}

func (rt *Router) getIndexGroups(cluster string) ([]indexGroup, error) {
	var igs, igresp []indexGroup
	var host string
	re := regexp.MustCompile(`^([\w\d\-_\.]+)-(\d{4}\.\d{2}\.\d{2}(-\d{2})*)`)

	//	rt.nodes.RLock()
	//	defer rt.nodes.RUnlock()
	t := time.Now()
	if cluster == "Snapshot" {
		host = rt.conf.Snapshot.Host
	} else if cluster == "Search" {
		host = rt.conf.Search.Host
	}

	response, err := rt.doGet(host+"_cat/indices/*-"+t.Format("2006.01.02")+"*,-.*/?format=json&h=index", cluster)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(response, &igs)
	fmt.Printf("%v\n", igs)
	if err != nil {
		return nil, err
	}
	for _, n := range igs {
		match := re.FindStringSubmatch(n.Index)
		n.Index = match[1] + "-*"
		igresp = append(igresp, n)
	}
	unique := removeDuplicates(igresp)
	fmt.Printf("%v\n", unique)
	return unique, nil

}

func (rt *Router) Barrel(ind_array IndicesInSnap, s3 bool) ([]string, []string) {
	var (
		k  int
		Sk int
		a  []string
		b  []string
	)

	for name, ind := range ind_array {
		if !s3 {
			for n := range rt.nodes.list {
				for m := range ind.Shards {
					k = rt.nodes.list[n] / ind.Shards[m]
					Sk = Sk + k
				}
			}

			if Sk > len(ind.Shards) {
				a = append(a, name)
			} else {
				b = append(b, name)
			}
		} else {
			a = append(a, name)
		}
	}
	return a, b
}

func (rt *Router) flattenMap(prefix string, nestedMap map[string]interface{}, flatMap map[string]string) {
	for key, value := range nestedMap {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		if subMap, ok := value.(map[string]interface{}); ok {
			if typeVal, exists := subMap["type"]; exists {
				flatMap[fullKey] = typeVal.(string)
			}

			if props, hasProps := subMap["properties"]; hasProps {
				rt.flattenMap(fullKey, props.(map[string]interface{}), flatMap)
			}
		}
	}
}

func getFile(fname string, size int64) ([]byte, error) {
	respFile, err := os.OpenFile(fname, os.O_RDONLY, 0)

	defer respFile.Close()
	if err != nil {
		return nil, err
	}
	bytes := make([]byte, size)
	respFile.Read(bytes)
	return bytes, nil
}

func allocateSpaceForFile(path string, size int64) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}

	if err := f.Truncate(size); err != nil {
		log.Fatal(err)
	}
}

func removeDuplicates(slice []indexGroup) []indexGroup {
	// Create a map to store unique elements
	seen := make(map[indexGroup]bool)
	var result []indexGroup

	// Loop through the slice, adding elements to the map if they haven't been seen before
	for _, val := range slice {
		if _, ok := seen[val]; !ok {
			seen[val] = true
			result = append(result, val)
		}
	}
	return result
}
