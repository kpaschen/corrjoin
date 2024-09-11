#!/bin/bash

set -eo pipefail

# This does not work yet on ubuntu.
# export DOCKER_BUILDKIT=1

docker build -f Dockerfile-receiver . -t localhost:5001/receiver:v0.1
docker push localhost:5001/receiver:v0.1

docker build -f Dockerfile-kafka . -t localhost:5001/worker:v0.1
docker push localhost:5001/worker:v0.1

