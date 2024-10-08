package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"strings"

	"github.com/opensearch-project/opensearch-go"
	"github.com/opensearch-project/opensearch-go/opensearchapi"
)

type OpenSearchRequest struct {
	Query       Query          `json:"query"`
	SearchAfter []interface{}  `json:"search_after"`
	Sort        []SortCriteria `json:"sort"`
}

type Query struct {
	Match Match `json:"match_phrase"`
}

type Match struct {
	Title string `json:"kubernetes.labels.app"`
}

type SortCriteria map[string]string

func main() {
	file, err := os.Create("/home/user/allLogs.json")
	defer file.Close()
	es, err := opensearch.NewClient(opensearch.Config{
		Addresses: []string{"https://elastic-x1-techno.apps.lmru.tech"},
		Username:  "",
		Password:  "",
	})
	if err != nil {
		log.Fatalf("Error creating the client: %s", err)
	}
	//initial request
	content := strings.NewReader(
		`{
				"sort": [
					{"@timestamp": "desc"}    
				],
				"_source": ["message"],
				"query": {
					"match_phrase": {
					"kubernetes.labels.app": "otp-auth-mobile"
				}
				}
			}`)
	search := opensearchapi.SearchRequest{
		Index: []string{"ausweis-*"},
		Body:  content,
	}

	searchResponse, err := search.Do(context.Background(), es)
	body, err := io.ReadAll(searchResponse.Body)
	var openSearchData OpenSearchResponse
	err = json.Unmarshal(body, &openSearchData)

	log.Printf("appending %d hits", len(openSearchData.HitsRoot.Hits))
	jsonData, err := json.MarshalIndent(openSearchData.HitsRoot.Hits, "", "  ")
	_, err = file.Write(jsonData)
	if err != nil {
		fmt.Println("Error writing to file:", err)
	}
	lastSearchCriteria := openSearchData.HitsRoot.Hits[len(openSearchData.HitsRoot.Hits)-1].SearchCriteria

	//next requests
	for {
		request := OpenSearchRequest{
			Query: Query{
				Match: Match{
					Title: "otp-auth-mobile",
				},
			},
			SearchAfter: lastSearchCriteria,
			Sort: []SortCriteria{
				{"@timestamp": "desc"},
			},
		}
		jsonData, err := json.MarshalIndent(request, "", "  ")
		if err != nil {
			fmt.Println(err)
			break
		}
		search = opensearchapi.SearchRequest{
			Index: []string{"ausweis-*"},
			Body:  bytes.NewReader(jsonData),
		}
		searchResponse, err = search.Do(context.Background(), es)
		body, err = io.ReadAll(searchResponse.Body)
		err = json.Unmarshal(body, &openSearchData)

		if len(openSearchData.HitsRoot.Hits) != 0 && compareSearchAfter(lastSearchCriteria, openSearchData.HitsRoot.Hits[len(openSearchData.HitsRoot.Hits)-1].SearchCriteria) {
			log.Printf("appending %d hits", len(openSearchData.HitsRoot.Hits))
			jsonData, err := json.MarshalIndent(openSearchData.HitsRoot.Hits, "", "  ")
			_, err = file.Write(jsonData)
			if err != nil {
				fmt.Println("Error writing to file:", err)
			}
			lastSearchCriteria = openSearchData.HitsRoot.Hits[len(openSearchData.HitsRoot.Hits)-1].SearchCriteria
		} else {
			log.Println("end of process")
			break
		}
	}

}

func compareSearchAfter(sa1, sa2 []interface{}) bool {
	if len(sa1) != len(sa2) {
		return false
	}
	for i, v := range sa1 {
		if !reflect.DeepEqual(v, sa2[i]) {
			return false
		}
	}
	return true
}

type OpenSearchResponse struct {
	Took     float64 `json:"took"`
	HitsRoot Hits    `json:"hits"`
}

type Hits struct {
	Hits     []Hit   `json:"hits"`
	MaxScore float64 `json:"max_score"`
}

type Hit struct {
	Source         Source        `json:"_source"`
	SearchCriteria []interface{} `json:"sort"`
}

type Source struct {
	Message string `json:"message"`
}
