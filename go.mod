module github.com/flant/elasticsearch-extractor

go 1.14

replace github.com/flant/elasticsearch-extractor/modules/front => ./modules/front

replace github.com/flant/elasticsearch-extractor/modules/router => ./modules/router

replace github.com/flant/elasticsearch-extractor/modules/config => ./modules/config

replace github.com/flant/elasticsearch-extractor/modules/version => ./modules/version

require (
	github.com/flant/elasticsearch-extractor/modules/config v0.0.0
	github.com/flant/elasticsearch-extractor/modules/front v0.0.0 // indirect
	github.com/flant/elasticsearch-extractor/modules/router v0.0.0
	github.com/flant/elasticsearch-extractor/modules/version v0.0.0
	github.com/uzhinskiy/lib.go v0.1.3 // indirect
	gopkg.in/yaml.v2 v2.3.0 // indirect
)
