#!/bin/bash

set -eo pipefail

rm -f receiver
go build -o receiver .
docker build -f Dockerfile . -t localhost:5001/receiver:v0.1
docker push localhost:5001/receiver:v0.1

