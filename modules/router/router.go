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
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"time"

	"github.com/flant/elasticsearch-extractor/modules/config"
	"github.com/flant/elasticsearch-extractor/modules/front"
	"github.com/flant/elasticsearch-extractor/modules/version"
	"github.com/uzhinskiy/lib.go/helpers"
)

type Filter struct {
	Field     string `json:"field,omitempty"`
	Operation string `json:"operation,omitempty"`
	Value     string `json:"value,omitempty"`
}

type Router struct {
	conf  config.Config
	nc    map[string]*http.Client
	nodes nodesArray
	sl    snapList
}

type apiRequest struct {
	Action string `json:"action,omitempty"` // Имя вызываемого метода*
	Values struct {
		Indices   []string `json:"indices,omitempty"`
		Repo      string   `json:"repo,omitempty"`
		OrderDir  string   `json:"odir,omitempty"`
		OrderType string   `json:"otype,omitempty"`
		Snapshot  string   `json:"snapshot,omitempty"`
		Index     string   `json:"index,omitempty"`
	} `json:"values,omitempty"`
	Search struct {
		Index       string            `json:"index,omitempty"`
		Cluster     string            `json:"cluster,omitempty"`
		Xql         string            `json:"xql,omitempty"`
		Fields      []string          `json:"fields,omitempty"`
		Filters     map[string]Filter `json:"filters,omitempty"`
		Mapping     []string          `json:"mapping,omitempty"`
		Timefields  []string          `json:"timefields,omitempty"`
		DateStart   string            `json:"date_start,omitempty"`
		DateEnd     string            `json:"date_end,omitempty"`
		SearchAfter string            `json:"search_after,omitempty"`
		Count       bool              `json:"count,omitempty"`
		Fname       string            `json:"fname,omitempty"`
	} `json:"search,omitempty"`
}

type snapStatus struct {
	Snapshots []struct {
		Snapshot string `json:"snapshot,omitempty"`
		State    string `json:"state,omitempty"`
		Indices  map[string]struct {
			ShardsStats struct {
				Total int `json:"total,omitempty"`
			} `json:"shards_stats,omitempty"`
			Stats struct {
				Total struct {
					Size int `json:"size_in_bytes,omitempty"`
				} `json:"total,omitempty"`
			} `json:"stats,omitempty"`
			Shards map[string]struct {
				Stats struct {
					Total struct {
						Size int `json:"size_in_bytes,omitempty"`
					} `json:"total,omitempty"`
				} `json:"stats,omitempty"`
			} `json:"shards,omitempty"`
		} `json:"indices,omitempty"`
	} `json:"snapshots,omitempty"`
}

type singleNode struct {
	Ip       string `json:"ip,omitempty"`
	Name     string `json:"name,omitempty"`
	Dt       string `json:"dt,omitempty"`
	Dtb      int
	Du       string `json:"du,omitempty"`
	Dup      string `json:"dup,omitempty"`
	D        string `json:"d,omitempty"`
	DiskFree int
}

type nodesStatus struct {
	nlist []singleNode
}

type nodesArray struct {
	//sync.RWMutex
	list []int
	max  int
	sum  int
}

type IndexInSnap struct {
	Name   string
	Size   int
	Shards []int
}

type indexGroup struct {
	Index string `json:"index,omitempty"`
}

type Cluster struct {
	Name string
	Host string
	Type string
}

type snapList []struct {
	Id          string `json:"id,omitempty"`
	Status      string `json:"status,omitempty"`
	End_epoch   string `json:"end_epoch,omitempty"`
	Start_epoch string `json:"start_epoch,omitempty"`
}

type scrollResponse struct {
	ScrollID string `json:"_scroll_id,omitempty"`
	HitsRoot Hits   `json:"hits"`
}

type HitsTotal struct {
	Value int64 `json:"value"`
}

type Hits struct {
	Total    HitsTotal `json:"total"`
	Hits     []Hit     `json:"hits"`
	MaxScore float64   `json:"max_score"`
}

type Hit struct {
	Source map[string]interface{} `json:"_source,omitempty"`
	Fields map[string]interface{} `json:"fields,omitempty"`
}

type IndicesInSnap map[string]*IndexInSnap

type ClusterHealth struct {
	ClusterName        string `json:"cluster_name,omitempty"`
	Status             string `json:"status,omitempty"`
	InitializingShards int    `json:"initializingShards,omitempty"`
	UnassignedShards   int    `json:"unassigned_shards,omitempty"`
}

type JSONRow map[string]interface{}

func Run(cnf config.Config) {
	rt := Router{}
	rt.conf = cnf
	rt.nc = make(map[string]*http.Client)
	rt.netClientPrepare()
	_, err := rt.getNodes()
	if err != nil {
		log.Println(err)
	}

	http.HandleFunc("/", rt.FrontHandler)
	http.HandleFunc("/api/", rt.ApiHandler)
	http.ListenAndServe(cnf.App.Bind+":"+cnf.App.Port, nil)
}

// web-ui
func (rt *Router) FrontHandler(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path

	remoteIP := helpers.GetIP(r.RemoteAddr, r.Header.Get("X-Real-IP"), r.Header.Get("X-Forwarded-For"))
	if file == "/" {
		file = "/index.html"
	}
	if file == "/search/" {
		file = "/search.html"
	}

	if strings.Contains(file, "/data/") {
		/*wdir, err := os.Getwd()
		if err != nil {
			log.Println(err)
		}*/

		fi, err := os.Lstat("/tmp" + file)
		if err != nil {
			http.Error(w, err.Error(), 404)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", 404, "\t", err.Error(), "\t", r.UserAgent())
			return
		}

		bytes, err := getFile("/tmp"+file, fi.Size())
		if err != nil {
			http.Error(w, err.Error(), 404)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", 404, "\t", err.Error(), "\t", r.UserAgent())
			return
		}

		contentType := mime.TypeByExtension(path.Ext("/tmp" + file))
		if contentType == "application/json" {
			w.Header().Set("Content-Type", "application/octet-stream")
		} else {
			w.Header().Set("Content-Type", contentType)
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Server", version.Version)

		w.Write(bytes)
		return
	}

	cFile := strings.Replace(file, "/", "", 1)
	data, err := front.Asset(cFile)
	if err != nil {
		http.Error(w, err.Error(), 404)
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", 404, "\t", err.Error(), "\t", r.UserAgent())
		return
	}

	/* отправить его клиенту */
	contentType := mime.TypeByExtension(path.Ext(cFile))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Server", version.Version)
	if strings.Contains(file, ".html") {
		t := template.Must(template.New("index").Parse(string(data)))
		t.Execute(w, rt.conf.App.Kibana)
	} else {
		w.Write(data)
	}
}

func (rt *Router) ApiHandler(w http.ResponseWriter, r *http.Request) {
	var request apiRequest

	defer r.Body.Close()
	remoteIP := helpers.GetIP(r.RemoteAddr, r.Header.Get("X-Real-IP"), r.Header.Get("X-Forwarded-For"))

	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "POST,OPTIONS")
	w.Header().Add("Access-Control-Allow-Credentials", "true")
	w.Header().Add("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
	w.Header().Add("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Server", version.Version)

	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusMethodNotAllowed, "\t", "Invalid request method ")
		return
	}
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
		return
	}
	switch request.Action {
	case "get_repositories":
		{
			response, err := rt.doGet(rt.conf.Snapshot.Host+"_cat/repositories?format=json", "Snapshot")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(response)
		}
	case "get_nodes":
		{
			nresp, err := rt.getNodes()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			j, _ := json.Marshal(nresp)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(j)
		}

	case "get_indices":
		{
			response, err := rt.doGet(rt.conf.Snapshot.Host+"extracted*/_recovery/", "Snapshot")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(response)
		}

	case "del_index":
		{
			if request.Values.Index == "" {
				msg := `{"error":"Required parameter Values.Index is missed"}`
				http.Error(w, msg, http.StatusBadRequest)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusBadRequest, "\t", msg)
				return
			}
			response, err := rt.doDel(rt.conf.Snapshot.Host+request.Values.Index, "Snapshot")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(response)
		}

	case "get_snapshots":
		{
			if request.Values.Repo == "" {
				msg := `{"error":"Required parameter Values.Repo is missed"}`
				http.Error(w, msg, http.StatusBadRequest)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusBadRequest, "\t", msg)
				return
			}

			response, err := rt.doGet(rt.conf.Snapshot.Host+"_cat/snapshots/"+request.Values.Repo+"?format=json", "Snapshot")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			var snap_list snapList
			err = json.Unmarshal(response, &snap_list)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			if !rt.conf.Snapshot.Include {
				j := 0
				for _, n := range snap_list {
					matched, err := regexp.MatchString(`^[\.]\S+`, n.Id)
					if err != nil {
						log.Println("Regex error for ", n.Id)
					}
					if !matched {
						snap_list[j] = n
						j++
					}

				}
				snap_list = snap_list[:j]

			}
			if request.Values.OrderType == "time" {

				if request.Values.OrderDir == "asc" {
					sort.Slice(snap_list[:], func(i, j int) bool {
						return snap_list[i].End_epoch < snap_list[j].End_epoch
					})
				} else {
					sort.Slice(snap_list[:], func(i, j int) bool {
						return snap_list[i].End_epoch > snap_list[j].End_epoch
					})
				}

			} else if request.Values.OrderType == "name" {

				if request.Values.OrderDir == "asc" {
					sort.Slice(snap_list[:], func(i, j int) bool {
						return snap_list[i].Id < snap_list[j].Id
					})

				} else {
					sort.Slice(snap_list[:], func(i, j int) bool {
						return snap_list[i].Id > snap_list[j].Id
					})
				}

			}
			rt.sl = snap_list
			j, _ := json.Marshal(snap_list)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(j)
		}

	case "get_snapshots_sorted":
		{
			if request.Values.OrderType == "time" {

				if request.Values.OrderDir == "asc" {
					sort.Slice(rt.sl[:], func(i, j int) bool {
						return rt.sl[i].End_epoch < rt.sl[j].End_epoch
					})
				} else {
					sort.Slice(rt.sl[:], func(i, j int) bool {
						return rt.sl[i].End_epoch > rt.sl[j].End_epoch
					})
				}

			} else if request.Values.OrderType == "name" {

				if request.Values.OrderDir == "asc" {
					sort.Slice(rt.sl[:], func(i, j int) bool {
						return rt.sl[i].Id < rt.sl[j].Id
					})

				} else {
					sort.Slice(rt.sl[:], func(i, j int) bool {
						return rt.sl[i].Id > rt.sl[j].Id
					})
				}

			}

			j, _ := json.Marshal(rt.sl)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent(), "\t", "Get Snapshots from cache")
			w.Write(j)
		}

	case "get_snapshot":
		{

			if request.Values.Repo == "" {
				msg := `{"error":"Required parameter Values.Repo is missed"}`
				http.Error(w, msg, http.StatusBadRequest)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusBadRequest, "\t", msg)
				return
			}

			if request.Values.Snapshot == "" {
				msg := `{"error":"Required parameter Values.Snapshot is missed"}`
				http.Error(w, msg, http.StatusBadRequest)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusBadRequest, "\t", msg)
				return
			}

			status_response, err := rt.doGet(rt.conf.Snapshot.Host+"_snapshot/"+request.Values.Repo+"/"+request.Values.Snapshot+"/_status", "Snapshot")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(status_response)
		}

	case "restore":
		{
			if request.Values.Repo == "" {
				msg := `{"error":"Required parameter Values.Repo is missed"}`
				http.Error(w, msg, http.StatusBadRequest)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusBadRequest, "\t", msg)
				return
			}

			if request.Values.Snapshot == "" {
				msg := `{"error":"Required parameter Values.Snapshot is missed"}`
				http.Error(w, msg, http.StatusBadRequest)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusBadRequest, "\t", msg)
				return
			}

			status_response, err := rt.doGet(rt.conf.Snapshot.Host+"_snapshot/"+request.Values.Repo+"/"+request.Values.Snapshot+"/_status", "Snapshot")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			var snap_status snapStatus
			err = json.Unmarshal(status_response, &snap_status)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			ch_response, err := rt.doGet(rt.conf.Snapshot.Host+"_cluster/health/extracted*", "Snapshot")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t_cluster/health/extracted*\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t_cluster/health/extracted*\t", r.UserAgent())

			var ch_status ClusterHealth
			err = json.Unmarshal(ch_response, &ch_status)

			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t_cluster/health/extracted*\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			// Если в кластере есть недовосстановленные индексы - прерываем
			if ch_status.InitializingShards > 5 || ch_status.UnassignedShards > 5 {
				msg := `{"error":"Indices will not be restored at now. Please wait"}`
				http.Error(w, msg, http.StatusTooManyRequests)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusTooManyRequests, "\t", msg)
				return
			}

			indices := make(IndicesInSnap)

			for _, iname := range request.Values.Indices {
				ind := snap_status.Snapshots[0].Indices[iname]
				indices[iname] = &IndexInSnap{}
				indices[iname].Size = ind.Stats.Total.Size
				if ind.ShardsStats.Total > 0 {
					for s := range snap_status.Snapshots[0].Indices[iname].Shards {
						indices[iname].Shards = append(indices[iname].Shards, snap_status.Snapshots[0].Indices[iname].Shards[s].Stats.Total.Size)
					}
				}
			}

			index_list_for_restore, index_list_not_restore := rt.Barrel(indices, rt.conf.Snapshot.IsS3)

			t := time.Now()
			req := map[string]interface{}{
				"ignore_unavailable":   false,
				"include_global_state": false,
				"include_aliases":      false,
				"rename_pattern":       "(.+)",
				"rename_replacement":   fmt.Sprintf("extracted_$1-%s", t.Format("02-01-2006")),
				"indices":              index_list_for_restore,
				"index_settings":       map[string]interface{}{"index.number_of_replicas": 0},
			}

			response, err := rt.doPost(rt.conf.Snapshot.Host+"_snapshot/"+request.Values.Repo+"/"+request.Values.Snapshot+"/_restore?wait_for_completion=false", req, "Snapshot")
			if err != nil {
				msg := fmt.Sprintf(`{"error":"%s"}`, err)
				http.Error(w, msg, 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", response)
				return
			}

			if len(index_list_not_restore) > 0 {
				msg := fmt.Sprintf(`{"message":"Indices '%v' will not be restored: Not enough space", "error":1}`, index_list_not_restore)
				w.Write([]byte(msg))
			}

			/*  Не создаем паттерны для восстановленных индексов
			for _, iname := range index_list_for_restore {
				if strings.Contains(iname, "v3") {
					ip_req := map[string]interface{}{
						"type": "index-pattern",
						"index-pattern": map[string]interface{}{
							"title":         "extracted_v3-*",
							"timeFieldName": "timestamp"}}

					ip_resp, err := rt.doPost(rt.conf.Snapshot.Host+".kibana/_doc/index-pattern:v3-080", ip_req, "Snapshot")
					if err != nil {
						msg := fmt.Sprintf(`{"error":"%s"}`, err)
						http.Error(w, msg, 500)
						log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\tcreate index-pattern\t", 500, "\t", err.Error(), "\t", ip_resp)
					}
				} else {
					ip_req := map[string]interface{}{
						"type": "index-pattern",
						"index-pattern": map[string]interface{}{
							"title":         "extracted_*",
							"timeFieldName": "@timestamp"}}

					ip_resp, err := rt.doPost(rt.conf.Snapshot.Host+".kibana/_doc/index-pattern:080", ip_req, "Snapshot")
					if err != nil {
						msg := fmt.Sprintf(`{"error":"%s"}`, err)
						http.Error(w, msg, 500)
						log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\tcreate index-pattern\t", 500, "\t", err.Error(), "\t", ip_resp)
					}

				}
			}
			*/

			msg := fmt.Sprintf(`{"message":"Indices '%v' will be restored", "error":0}`, index_list_for_restore)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write([]byte(msg))

		}
		/*  ---- search --- */
	case "get_clusters":
		{
			var cl []Cluster
			cl = append(cl, Cluster{rt.conf.Snapshot.Name, rt.conf.Snapshot.Host, "Snapshot"})
			cl = append(cl, Cluster{rt.conf.Search.Name, rt.conf.Search.Host, "Search"})
			j, _ := json.Marshal(cl)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(j)
		}
	case "get_index_groups":
		{
			response, err := rt.getIndexGroups(request.Search.Cluster)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			j, _ := json.Marshal(response)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent())
			w.Write(j)
		}

	case "get_mapping":
		{
			t := time.Now()
			var (
				fullm map[string]interface{}
				m     map[string]interface{}
				host  string
			)
			if request.Search.Cluster == "Snapshot" {
				host = rt.conf.Snapshot.Host
			} else if request.Search.Cluster == "Search" {
				host = rt.conf.Search.Host
			}
			flatMap := make(map[string]string)
			response, err := rt.doGet(host+request.Search.Index+"*"+t.Format("2006.01.02")+"*,"+request.Search.Index+"*"+t.Format("02-01-2006")+"*/_mapping", "Search")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			err = json.Unmarshal(response, &fullm)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			for _, v := range fullm {
				m = v.(map[string]interface{})
			}

			if mapping, hasMap := m["mappings"]; hasMap {
				rt.flattenMap("", mapping.(map[string]interface{})["properties"].(map[string]interface{}), flatMap)
			}

			j, _ := json.Marshal(flatMap)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent(), "\t", host+request.Search.Index)
			w.Write(j)
		}

	case "search":
		{
			var (
				use_source string
				query      string
				filters    string
				must_not   string
				xql        string
				full_query string
				timefield  string
				sort       string
				tf         string
				fields     string
				req        map[string]interface{}
				host       string
			)
			if request.Search.Cluster == "Snapshot" {
				host = rt.conf.Snapshot.Host
			} else if request.Search.Cluster == "Search" {
				host = rt.conf.Search.Host
			}

			ds, _ := time.Parse("2006-01-02 15:04:05 (MST)", request.Search.DateStart+" (MSK)")
			de, _ := time.Parse("2006-01-02 15:04:05 (MST)", request.Search.DateEnd+" (MSK)")

			if len(request.Search.Fields) == 0 {
				use_source = `"_source": true`
			} else {
				use_source = `"_source": false`
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

			full_query = fmt.Sprintf(`{"size": 500, %s, %s, %s, %s }`, sort, use_source, fields, query)
			if request.Search.Count {
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent(), "\t", host+request.Search.Index, "\t", "action: Count", "\tquery: ", "{"+query+"}")
				_ = json.Unmarshal([]byte("{"+query+"}"), &req)
				cresponse, err := rt.doPost(host+request.Search.Index+"/_count", req, "Search")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
					return
				}

				w.Write(cresponse)
			} else {
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent(), "\t", host+request.Search.Index, "\t", "action: Search", "\tquery: ", full_query)
				_ = json.Unmarshal([]byte(full_query), &req)
				sresponse, err := rt.doPost(host+request.Search.Index+"/_search", req, "Search")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
					return
				}
				w.Write(sresponse)
			}

		}

	case "prepare_csv":
		{
			//allocateSpaceForFile("/tmp/data/"+request.Search.Fname+".csv", 100)
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

			err = json.Unmarshal([]byte(full_query), &req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
			sresponse, err := rt.doPost(host+request.Search.Index+"/_search?scroll=10m", req, "Search")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			f, err := os.OpenFile("/tmp/data/"+request.Search.Fname+".csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()

			if len(request.Search.Timefields) > 0 {
				f.WriteString(request.Search.Timefields[0] + `;` + strings.Join(fields_list, ";") + "\n")
			} else {
				f.WriteString(strings.Join(fields_list, ";") + "\n")
			}

			err = json.Unmarshal(sresponse, &scrollresponse)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			if len(scrollresponse.HitsRoot.Hits) > 0 {
				var data any
				for _, row := range scrollresponse.HitsRoot.Hits {
					fileInfo, err := os.Stat("/tmp/data/" + request.Search.Fname + ".csv")
					if err != nil {
						log.Println(err)
						return
					}

					if fileInfo.Size() > rt.conf.Search.FileLimit.Size {
						return
					}
					if len(request.Search.Fields) == 0 {
						f.WriteString(fmt.Sprintf("%v;", row.Source[request.Search.Timefields[0]]))
					} else {
						f.WriteString(fmt.Sprintf("%v;", row.Fields[request.Search.Timefields[0]]))
					}
					for _, fm := range fields_list {
						if len(request.Search.Fields) == 0 {
							data = row.Source[fm]
						} else {
							data = row.Fields[fm]
						}

						if data == nil {
							f.WriteString(fmt.Sprintf("%s;", "--"))
						} else {
							switch reflect.TypeOf(data).Kind() {
							case reflect.Slice:
								{
									s := reflect.ValueOf(data)
									var ss string
									for i := 0; i < s.Len(); i++ {
										ss = ss + fmt.Sprintf("%v, ", s.Index(i))
									}
									ss = strings.TrimSuffix(ss, ", ")
									ss = strings.Replace(ss, "\n", "", -1)
									ss = strings.Replace(ss, "\"", "\"\"", -1)
									f.WriteString(fmt.Sprintf(`"%s";`, ss))

								}
							case reflect.String:
								{
									f.WriteString(fmt.Sprintf(`"%v";`, strings.Replace(strings.Replace(data.(string), "\n", "", -1), "\"", "\"\"", -1)))
								}
							default:
								{
									f.WriteString(fmt.Sprintf("%v;", data))
								}
							}

						}

					}
					f.WriteString("\n")
				}
			}

			if scrollresponse.ScrollID != "" {
				scroll := map[string]interface{}{"scroll": "10m", "scroll_id": scrollresponse.ScrollID}
				for i := 0; i < rt.conf.Search.FileLimit.Rows/10000; i++ {
					sresponse, err := rt.doPost(host+"_search/scroll", scroll, "Search")
					if err != nil {
						log.Println("Failed to get scroll batch: ", err)
						return
					}
					_ = json.Unmarshal(sresponse, &scrollresponse)
					if len(scrollresponse.HitsRoot.Hits) == 0 {
						_, _ = rt.doDel(request.Search.Cluster+"_search/scroll/"+scrollresponse.ScrollID, "Search")
						break
					}
					if len(scrollresponse.HitsRoot.Hits) > 0 {

						var data any
						for _, row := range scrollresponse.HitsRoot.Hits {
							fileInfo, err := os.Stat("/tmp/data/" + request.Search.Fname + ".csv")
							if err != nil {
								log.Println(err)
								return
							}
							if fileInfo.Size() > rt.conf.Search.FileLimit.Size {
								return
							}

							if len(request.Search.Fields) == 0 {
								f.WriteString(fmt.Sprintf("%v;", row.Source[request.Search.Timefields[0]]))
							} else {
								f.WriteString(fmt.Sprintf("%v;", row.Fields[request.Search.Timefields[0]]))
							}
							for _, fm := range fields_list {
								if len(request.Search.Fields) == 0 {
									data = row.Source[fm]
								} else {
									data = row.Fields[fm]
								}

								if data == nil {
									f.WriteString(fmt.Sprintf("%s;", "--"))
								} else {
									switch reflect.TypeOf(data).Kind() {
									case reflect.Slice:
										{
											s := reflect.ValueOf(data)
											var ss string
											for i := 0; i < s.Len(); i++ {
												ss = ss + fmt.Sprintf("%v, ", s.Index(i))
											}
											ss = strings.TrimSuffix(ss, ", ")
											ss = strings.Replace(ss, "\n", "", -1)
											ss = strings.Replace(ss, "\"", "\"\"", -1)
											f.WriteString(fmt.Sprintf(`"%s";`, ss))
										}
									case reflect.String:
										{
											f.WriteString(fmt.Sprintf(`"%v";`, strings.Replace(strings.Replace(data.(string), "\n", "", -1), "\"", "\"\"", -1)))
										}
									default:
										{
											f.WriteString(fmt.Sprintf("%v;", data))
										}
									}
								}
							}
							f.WriteString("\n")
						}
					}
				}

			}
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent(), "\t", host+request.Search.Index, "\t", "action: CSV", "\tquery: ", full_query, "\tfile: ", request.Search.Fname)
			w.Write([]byte("Done"))
		}
	case "prepare_json":
		{
			err := rt.saveHintsToJsonFile(request)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", r.UserAgent(), "\t", "action: JSON")

			w.Write([]byte("Done"))
		}

	default:
		{
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", http.StatusServiceUnavailable, "\t", "Invalid request method ")
			return

		}

	}
}
