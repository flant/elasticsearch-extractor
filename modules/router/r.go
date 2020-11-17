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
	"strings"

	"time"

	"github.com/uzhinskiy/extractor/modules/config"
	"github.com/uzhinskiy/extractor/modules/front"
	"github.com/uzhinskiy/extractor/modules/version"
	"github.com/uzhinskiy/lib.go/helpers"
)

type Router struct {
	conf  config.Config
	nc    *http.Client
	nodes nodesArray
}

type apiRequest struct {
	Action string `json:"action,omitempty"` // Имя вызываемого метода*
	Values struct {
		Indices  []string `json:"indices,omitempty"`
		Repo     string   `json:"repo,omitempty"`
		Snapshot string   `json:"snapshot,omitempty"`
		Index    string   `json:"index,omitempty"`
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
	http.ListenAndServe(":"+cnf.App.Port, nil)
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
		log.Println(err)
	}

	log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", r.UserAgent())
	/* отправить его клиенту */
	contentType := mime.TypeByExtension(path.Ext(cFile))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Server", version.Version)
	w.Write(data)
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
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", http.StatusServiceUnavailable, "\t", "Invalid request method ", "\t", r.UserAgent())
		return
	}
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, err.Error(), 500)
		log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
		return
	}

	log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 200, "\t", r.UserAgent())

	switch request.Action {
	case "get_repositories":
		{
			response, err := rt.doGet(rt.conf.Elastic.Host + "_cat/repositories?format=json")
			if err != nil {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}
			w.Write(response)
		}
	case "get_nodes":
		{

			nresp, err := rt.getNodes()

			if err != nil {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}

			j, _ := json.Marshal(nresp)
			w.Write(j)
		}

	case "get_indices":
		{
			//response, err := rt.doGet(rt.conf.Elastic.Host + "_cat/indices/restored*?s=i&format=json")
			response, err := rt.doGet(rt.conf.Elastic.Host + "extracted*/_recovery/")
			if err != nil {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}

			w.Write(response)
		}

	case "del_index":
		{
			if request.Values.Index == "" {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}
			response, err := rt.doDel(rt.conf.Elastic.Host + request.Values.Index)
			if err != nil {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}

			w.Write(response)
		}

	case "get_snapshots":
		{
			if request.Values.Repo == "" {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}
			response, err := rt.doGet(rt.conf.Elastic.Host + "_cat/snapshots/" + request.Values.Repo + "?format=json")
			if err != nil {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}
			w.Write(response)
		}

	case "get_snapshot":
		{

			if request.Values.Repo == "" {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}

			if request.Values.Snapshot == "" {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}

			status_response, err := rt.doGet(rt.conf.Elastic.Host + "_snapshot/" + request.Values.Repo + "/" + request.Values.Snapshot + "/_status")
			if err != nil {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}
			w.Write(status_response)
		}

	case "restore":
		{

			if request.Values.Repo == "" {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}

			if request.Values.Snapshot == "" {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}

			status_response, err := rt.doGet(rt.conf.Elastic.Host + "_snapshot/" + request.Values.Repo + "/" + request.Values.Snapshot + "/_status")
			if err != nil {
				http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", r.UserAgent())
				return
			}
			var snap_status snapStatus
			_ = json.Unmarshal(status_response, &snap_status)

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

			index_list_for_restore, index_list_not_restore := rt.Barrel(indices)
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
				msg := fmt.Sprintf("{\"error\":\"%s\"}", err)
				http.Error(w, msg, 500)
				//http.Error(w, err.Error(), 500)
				log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", request.Action, "\t", 500, "\t", err.Error(), "\t", response)
				return
			}

			if len(index_list_not_restore) > 0 {
				msg := fmt.Sprintf("{\"message\":\"Indices '%v' will not be restored: Not enough space\", \"error\":1}", index_list_not_restore)
				w.Write([]byte(msg))
			}

			msg := fmt.Sprintf("{\"message\":\"Indices '%v' will be restored\", \"error\":0}", index_list_for_restore)
			w.Write([]byte(msg))

		}

	default:
		{
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			log.Println(remoteIP, "\t", r.Method, "\t", r.URL.Path, "\t", http.StatusServiceUnavailable, "\t", "Invalid request method ", "\t", r.UserAgent())
			return

		}

	}
}

func (rt *Router) Barrel(array IndicesInSnap) ([]string, []string) {
	var (
		k  int
		Sk int
		a  []string
		b  []string
	)

	for name, ind := range array {
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
	}
	return a, b
}

func (rt *Router) getNodes() ([]singleNode, error) {

	var nresp []singleNode
	var na nodesArray

	//	rt.nodes.RLock()
	//	defer rt.nodes.RUnlock()

	response, err := rt.doGet(rt.conf.Elastic.Host + "_cat/nodes?format=json&bytes=b&h=ip,name,dt,du,dup,d&s=name")
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
