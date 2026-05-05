#!/bin/bash

rm turbomesh-linux-amd64
GOOS=linux GOARCH=amd64 go build -o turbomesh-linux-amd64 .