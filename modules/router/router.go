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
	"path"
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

type Router struct {
	conf  config.Config
	nc    *http.Client
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

type snapList []struct {
	Id          string `json:"id,omitempty"`
	Status      string `json:"status,omitempty"`
	End_epoch   string `json:"end_epoch,omitempty"`
	Start_epoch string `json:"start_epoch,omitempty"`
}

type IndicesInSnap map[string]*IndexInSnap

func Run(cnf config.Config) {
	rt := Router{}
	rt.conf = cnf
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
	cFile := strings.Replace(file, "/", "", 1)
	data, err := front.Asset(cFile)
	if err != nil {
		http.Error(w, err.Error(), 404)
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", 404, "\t", err.Error(), "\t", r.UserAgent())
		return
	}

	log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", r.UserAgent())
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
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", http.StatusMethodNotAllowed, "\t", "Invalid request method ")
		return
	}
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", http.StatusInternalServerError, "\t", err.Error())
		return
	}

	switch request.Action {
	case "get_repositories":
		{
			response, err := rt.doGet(rt.conf.Elastic.Host + "_cat/repositories?format=json")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
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
			w.Write(j)
		}

	case "get_indices":
		{
			response, err := rt.doGet(rt.conf.Elastic.Host + "extracted*/_recovery/")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

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
			response, err := rt.doDel(rt.conf.Elastic.Host + request.Values.Index)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}

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

			response, err := rt.doGet(rt.conf.Elastic.Host + "_cat/snapshots/" + request.Values.Repo + "?format=json")
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

			if !rt.conf.Elastic.Include {
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

			log.Println("Get Snapshots from cache")

			j, _ := json.Marshal(rt.sl)

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

			status_response, err := rt.doGet(rt.conf.Elastic.Host + "_snapshot/" + request.Values.Repo + "/" + request.Values.Snapshot + "/_status")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", http.StatusInternalServerError, "\t", err.Error())
				return
			}
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

			status_response, err := rt.doGet(rt.conf.Elastic.Host + "_snapshot/" + request.Values.Repo + "/" + request.Values.Snapshot + "/_status")
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

			index_list_for_restore, index_list_not_restore := rt.Barrel(indices, rt.conf.Elastic.IsS3)
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

			response, err := rt.doPost(rt.conf.Elastic.Host+"_snapshot/"+request.Values.Repo+"/"+request.Values.Snapshot+"/_restore?wait_for_completion=false", req)
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

			for _, iname := range index_list_for_restore {
				if strings.Contains(iname, "v3") {
					ip_req := map[string]interface{}{
						"type": "index-pattern",
						"index-pattern": map[string]interface{}{
							"title":         "extracted_v3-*",
							"timeFieldName": "timestamp"}}

					ip_resp, err := rt.doPost(rt.conf.Elastic.Host+".kibana/_doc/index-pattern:v3-080", ip_req)
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

					ip_resp, err := rt.doPost(rt.conf.Elastic.Host+".kibana/_doc/index-pattern:080", ip_req)
					if err != nil {
						msg := fmt.Sprintf(`{"error":"%s"}`, err)
						http.Error(w, msg, 500)
						log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\tcreate index-pattern\t", 500, "\t", err.Error(), "\t", ip_resp)
					}

				}
			}

			msg := fmt.Sprintf(`{"message":"Indices '%v' will be restored", "error":0}`, index_list_for_restore)
			w.Write([]byte(msg))

		}

	default:
		{
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", http.StatusServiceUnavailable, "\t", "Invalid request method ")
			return

		}

	}
}
