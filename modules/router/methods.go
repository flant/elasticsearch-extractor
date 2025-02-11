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
	"strings"

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

	response, err := rt.doGet(host+"_cat/indices/*-"+t.Format("2006.01.02")+"*,*-"+t.Format("02-01-2006")+",-.*/?format=json&h=index", cluster)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(response, &igs)
	if err != nil {
		return nil, err
	}
	for _, n := range igs {
		match := re.FindStringSubmatch(n.Index)
		n.Index = match[1] + "-*"
		igresp = append(igresp, n)
	}
	unique := removeDuplicates(igresp)
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

func (rt *Router) saveHintsToJsonFile(request apiRequest) error {
	var (
		use_source     string
		query          string
		filters        string
		must_not       string
		xql            string
		full_query     string
		timefield      string
		sort           string
		tf             string
		fields         string
		req            map[string]interface{}
		scrollresponse scrollResponse
		fields_list    []string
		host           string
		request_batch  int64
	)

	request_batch = rt.conf.Search.RequestBatch

	if request.Search.Cluster == "Snapshot" {
		host = rt.conf.Snapshot.Host
	} else if request.Search.Cluster == "Search" {
		host = rt.conf.Search.Host
	}

	ds, _ := time.Parse("2006-01-02 15:04:05 (MST)", request.Search.DateStart+" (MSK)")
	de, _ := time.Parse("2006-01-02 15:04:05 (MST)", request.Search.DateEnd+" (MSK)")

	if len(request.Search.Fields) == 0 {
		use_source = `"_source": true`
		fields_list = request.Search.Mapping
	} else {
		use_source = `"_source": false`
		fields_list = request.Search.Fields
	}

	for _, f := range request.Search.Filters {
		if f.Operation == "is" {
			filters += `{ "wildcard": {"` + f.Field + `.keyword": {"value": "` + f.Value + `" } } },`
		} else if f.Operation == "exists" {
			filters += `{ "exists": {"field":"` + f.Field + `" } },`
		} else if f.Operation == "is_not" {
			must_not += `{ "match_phrase": {"` + f.Field + `":"` + f.Value + `" } },`
		} else if f.Operation == "does_not_exists" {
			must_not += `{ "exists": {"field":"` + f.Field + `" } },`
		}
	}
	filters += `{"match_all": {}}`
	must_not, _ = strings.CutSuffix(must_not, ",")

	if request.Search.Xql != "" {
		xql = `{ "simple_query_string": { "query": "` + request.Search.Xql + `" } }`
	}

	if len(request.Search.Timefields) > 0 {
		timefield = request.Search.Timefields[0]
		fields = `"fields": ["` + timefield + `", "` + strings.Join(request.Search.Fields, "\", \"") + `" ]`
		sort = `"sort": [ {"` + timefield + `": "desc" } ]`
		tf = `{ "range": { "` + timefield + `": {
						   "gte": "` + ds.Format("2006-01-02T15:04:05.000Z") + `",
						   "lte": "` + de.Format("2006-01-02T15:04:05.000Z") + `",
						   "format": "strict_date_optional_time" } } },`
	} else {
		sort = ""
		tf = ""
		fields = `"fields": ["` + strings.Join(request.Search.Fields, "\", \"") + `" ]`
	}
	query = fmt.Sprintf(`"query": { "bool": { "must": [ %s ],"filter": [  %s  %s ], "should": [],"must_not": [ %s ] }}`, xql, tf, filters, must_not)

	full_query = fmt.Sprintf(`{"size": %d, %s, %s, %s, %s }`, request_batch, sort, use_source, fields, query)
	fmt.Println("full_query", full_query)
	err := json.Unmarshal([]byte(full_query), &req)
	if err != nil {
		return err
	}

	fileName := request.Search.Fname + ".json"
	filePath := "/tmp/data/" + fileName
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	scrollId := ""
	for {
		sresponse := []byte{}
		if scrollId == "" {
			r, err := rt.doPost(host+request.Search.Index+"/_search?scroll=10m", req, "Search")
			if err != nil {
				return err
			}
			sresponse = r
		} else {
			scroll := map[string]interface{}{"scroll": "10m", "scroll_id": scrollId}
			r, err := rt.doPost(host+"_search/scroll", scroll, "Search")
			if err != nil {
				return err
			}
			sresponse = r
		}

		err = json.Unmarshal(sresponse, &scrollresponse)
		if err != nil {
			return err
		}

		scrollId = scrollresponse.ScrollID

		log.Println("Hints total", scrollresponse.HitsRoot.Total.Value)
		log.Println("Hints req len", len(scrollresponse.HitsRoot.Hits))
		log.Println("scrollId", scrollId)

		if len(scrollresponse.HitsRoot.Hits) == 0 {
			_, _ = rt.doDel(request.Search.Cluster+"_search/scroll/"+scrollresponse.ScrollID, "Search")
			log.Println("Search is done!")
			break
		}

		for _, hint := range scrollresponse.HitsRoot.Hits {
			var row = make(JSONRow)
			if len(request.Search.Fields) == 0 {
				row[request.Search.Timefields[0]] = hint.Source[request.Search.Timefields[0]]
			} else {
				row[request.Search.Timefields[0]] = hint.Fields[request.Search.Timefields[0]]
			}
			for _, field := range fields_list {
				if len(request.Search.Fields) == 0 {
					row[field] = hint.Source[field]
				} else {
					row[field] = hint.Fields[field]
				}

				if row[field] == nil {
					row[field] = "--"
				}
			}

			jsonData, err := json.Marshal(row)
			if err != nil {
				return err
			}

			_, err = f.WriteString(string(jsonData) + "\n")
			if err != nil {
				return err
			}
		}

		fileInfo, err := os.Stat(filePath)
		if err != nil {
			return err
		}

		if fileInfo.Size() > rt.conf.Search.FileLimit.Size {
			return errors.New(fmt.Sprintf("file %s with size %d is too big", filePath, fileInfo.Size()))
		}
	}

	return nil
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
