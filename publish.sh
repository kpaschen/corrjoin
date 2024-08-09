#!/bin/bash

set -eo pipefail

docker build -f Dockerfile . -t localhost:5001/receiver:v0.1
docker push localhost:5001/receiver:v0.1

