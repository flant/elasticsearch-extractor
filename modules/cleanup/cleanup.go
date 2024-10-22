// Copyright © 2024 Uzhinskiy Boris
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

package cleanup

import (
	"io/ioutil"
	"log"
	"os"
	"time"
)

var cutoff = 1 * time.Hour

func Run() {
	for {
		now := time.Now()
		files, err := ioutil.ReadDir("/tmp/data")
		if err != nil {
			log.Panicln(err)
			return
		}

		for _, file := range files {
			if diff := now.Sub(file.ModTime()); diff > cutoff {
				log.Printf("Deleting %s which is %s old\n", file.Name(), diff)
				err := os.Remove("/tmp/data/" + file.Name())
				if err != nil {
					log.Println(err)
					return
				}
			}
		}

		// do some job
		time.Sleep(10 * time.Minute)
	}
}
