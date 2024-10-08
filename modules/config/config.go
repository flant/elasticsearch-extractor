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

package config

import (
	"io/ioutil"
	"log"
	"regexp"

	"gopkg.in/yaml.v2"
)

type Config struct {
	App struct {
		Port       string `yaml:"port"`
		Bind       string `yaml:"bind"`
		Kibana     string `yaml:"kibana"`
		TimeOut    int    `yaml:"-"`
		TimeOutRaw *int   `yaml:"timeout"`
	} `yaml:"app"`
	Snapshot struct {
		Host               string `yaml:"host"`
		Name               string
		SSL                bool   `yaml:"ssl"`
		Username           string `yaml:"username"`
		Password           string `yaml:"password"`
		CAcert             string `yaml:"ca_cert"`
		ClientCert         string `yaml:"client_cert"`
		ClientKey          string `yaml:"client_key"`
		InsecureSkipVerify bool   `yaml:"insecure"`
		Include            bool   `yaml:"include_system"`
		IsS3               bool   `yaml:"is_s3"`
	} `yaml:"snapshot"`
	Search struct {
		Host               string `yaml:"host,omitempty"`
		Name               string
		SSL                bool   `yaml:"ssl,omitempty"`
		Username           string `yaml:"username,omitempty"`
		Password           string `yaml:"password,omitempty"`
		CAcert             string `yaml:"ca_cert,omitempty"`
		ClientCert         string `yaml:"client_cert,omitempty"`
		ClientKey          string `yaml:"client_key,omitempty"`
		InsecureSkipVerify bool   `yaml:"insecure,omitempty"`
	} `yaml:"search,omitempty"`
}

func Parse(f string) Config {
	var c Config
	var re = regexp.MustCompile(`(?m)^https*://(?P<host>[\w\d-\._]+)*:*[\d]*/*$`)
	template := []byte("$host\n")

	yamlBytes, err := ioutil.ReadFile(f)
	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(yamlBytes, &c)
	if err != nil {
		log.Fatal(err)
	}

	if c.App.Port == "" {
		c.App.Port = "9400"
	}

	if c.App.Bind == "" {
		c.App.Bind = "0.0.0.0"
	}

	c.App.TimeOut = 30
	if c.App.TimeOutRaw != nil {
		c.App.TimeOut = *c.App.TimeOutRaw
	}

	if c.Snapshot.Host == "" {
		c.Snapshot.Host = "http://127.0.0.1:9200/"
	}
	s0 := re.FindSubmatchIndex([]byte(c.Snapshot.Host))
	c.Snapshot.Name = string(re.Expand([]byte{}, template, []byte(c.Snapshot.Host), s0))

	if c.Search.Host == "" {
		c.Search.Host = "http://127.0.0.1:9200/"
	}
	s1 := re.FindSubmatchIndex([]byte(c.Search.Host))
	c.Search.Name = string(re.Expand([]byte{}, template, []byte(c.Search.Host), s1))

	return c
}
