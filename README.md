# go-bsb-cams
Simple program to take and output the Bigscreen Beyond 2e cameras to a webserver to be used with eyetracking software.

## Usage
Pre-Compiled Binares are in the Releases Section, and can be run out of the box with `./go-bsb-cams`

The code by default outputs to `localhost:8080/stream` but can be configured with the `-port` flag.

To run or build the src with golang:

Clone This repo and get the dependencies with: `go get .`

Execute the following command within the root directory: `go run main.go` to run as a go program

Alternatively, the program can be built with `go build` and run via the resulting executable.
