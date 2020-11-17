module github.com/uzhinskiy/extractor

go 1.14

replace github.com/uzhinskiy/extractor/modules/front => ./modules/front

replace github.com/uzhinskiy/extractor/modules/router => ./modules/router

replace github.com/uzhinskiy/extractor/modules/config => ./modules/config

replace github.com/uzhinskiy/extractor/modules/version => ./modules/version

require (
	github.com/uzhinskiy/extractor/modules/config v0.0.0
	github.com/uzhinskiy/extractor/modules/front v0.0.0 // indirect
	github.com/uzhinskiy/extractor/modules/router v0.0.0
	github.com/uzhinskiy/extractor/modules/version v0.0.0
	github.com/uzhinskiy/lib.go v0.1.3 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
)
