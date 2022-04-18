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

	"gopkg.in/yaml.v2"
)

type Config struct {
	App struct {
		Port       string `yaml:"port"`
		Bind       string `yaml:"bind"`
		TimeOut    int    `yaml:"-"`
		TimeOutRaw *int   `yaml:"timeout"`
	} `yaml:"app"`
	Elastic struct {
		Host       string `yaml:"host"`
		SSL        bool   `yaml:"ssl"`
		Username   string `yaml:"username"`
		Password   string `yaml:"password"`
		CAcert     string `yaml:"ca_cert"`
		ClientCert string `yaml:"client_cert"`
		ClientKey  string `yaml:"client_key"`
		Include    bool   `yaml:"include_system"`
	} `yaml:"elastic"`
}

func Parse(f string) Config {
	var c Config
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

	if c.Elastic.Host == "" {
		c.Elastic.Host = "http://127.0.0.1:9200/"
	}

	return c
}
