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

package main

import (
	"errors"
	"flag"
	"log"
	"os"

	"github.com/flant/elasticsearch-extractor/modules/cleanup"
	"github.com/flant/elasticsearch-extractor/modules/config"
	"github.com/flant/elasticsearch-extractor/modules/router"
	"github.com/flant/elasticsearch-extractor/modules/version"
	//TODO: переход на slog и JSON
	//"log/slog"
)

var (
	configfile string
	vBuild     string
	cnf        config.Config
	hostname   string
)

func init() {
	flag.StringVar(&configfile, "config", "main.yml", "Read configuration from this file")
	flag.StringVar(&configfile, "f", "main.yml", "Read configuration from this file")
	vers := flag.Bool("V", false, "Show version")
	flag.Parse()
	if *vers {
		print("version: ", version.Version, "( ", vBuild, " )\n")
		os.Exit(0)
	}

	hostname, _ = os.Hostname()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix(hostname + "\tapi.version:" + version.Version + "\t")

	log.Println("Bootstrap: build num.", vBuild)

	cnf = config.Parse(configfile)
	log.Println("Bootstrap: successful parsing config file.")
	if _, err := os.Stat("/tmp/data"); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir("/tmp/data", os.ModePerm)
		if err != nil {
			log.Fatalf("cannot create directory: %s\n", err)
		}
	}

}

func main() {
	go cleanup.Run()
	router.Run(cnf)
}
