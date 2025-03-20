#!/bin/sh

set -euo pipefail

# Flavor determines the values files to use for helm. Use local for a local (test) cluster,
# cloud for a "real" cluster. For 'cloud', take a look at the values-cloud.yaml files in the
# helm charts to make sure the values are good for you.
flavor=${FLAVOR:-local}

# We build a local apache image and an image for the mattermost loadtesting tool.
# These need to be placed in an image registry.
# This needs to match the image.registry setting in your values-${flavor}.yaml files.
imageregistry=${IMAGEREGISTRY:-localhost:5001}

# Building the mattermost loadtesting tool involves checking it out from git.
# LTHOME is the parent directory of where you want to check out the loadtesting code.
LTHOME=${LTHOME:-/home/developer/code}

repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "mattermost") .url')
if [ -z $repo_exists ]; then
   helm repo add mattermost https://helm.mattermost.com
fi
helm repo update

# Install postgres so mattermost can use it
ns_exists=$(kubectl get namespace -o name | grep postgres)
if [ -z $ns_exists ]; then 
   kubectl create ns postgres
fi

# The postgres chart also contains the mattermost db secret, so make sure
# that namespace also exists.
ns_exists=$(kubectl get namespace -o name | grep mattermost)
if [ -z $ns_exists ]; then 
   kubectl create ns mattermost
fi
(
cd helm/postgres &&
helm upgrade --install postgres . -f values-${flavor}.yaml
)

# Install mattermost
ns_exists=$(kubectl get namespace -o name | grep mattermost-operator)
if [ -z $ns_exists ]; then 
   kubectl create ns mattermost-operator
fi
helm upgrade --install mattermost-operator mattermost/mattermost-operator -n mattermost-operator -f mattermost-config.yaml

kubectl apply -f mattermost-installation.yaml

(
cd apache
docker build -f Dockerfile -t ${imageregistry}/mmapache:latest .
docker push ${imageregistry}/mmapache:latest
)

# Set up apache in kubernetes.
# This is so we can attach an apache exporter to it for monitoring.
ns_exists=$(kubectl get namespace -o name | grep apache)
if [ -z $ns_exists ]; then 
   kubectl create ns apache
fi
(
cd helm/apache &&
helm upgrade --install apache . -f values-${flavor}.yaml
)

# Set up the mattermost load test. This clones a git repository and builds
# a docker image from there.
# First, we need to create an admin user for mattermost.
mmpod=$(kubectl -n mattermost get pods -o=name | grep mm-corrjoin)
kubectl -n mattermost exec $mmpod -c mattermost -- /mattermost/bin/mmctl --local user create --email=nobody@nephometrics.com --username=mmadmin --password=mmadmin123
kubectl -n mattermost exec $mmpod -c mattermost -- /mattermost/bin/mmctl --local system-admin mmadmin

mkdir -p $LTHOME
(
cd $LTHOME
if [ ! -d mattermost-load-test-ng ]; then
  git clone https://github.com/mattermost/mattermost-load-test-ng
fi
cd mattermost-load-test-ng
git pull
)
cp loadtest/Dockerfile $LTHOME/mattermost-load-test-ng/Dockerfile-lt
cp loadtest/config.json $LTHOME/mattermost/-load-test-ng/config
cp loadtest/simplecontroller.json $LTHOME/mattermost-loadtest-ng/config
(
cd $LTHOME/mattermost-load-test-g
docker build -f Dockerfile-lt -t ${imageregistry}/mmloadtest:latest .
docker push ${imageregistry}/mmloadtest:latest
)

ns_exists=$(kubectl get namespace -o name | grep mattermost-lt)
if [ -z $ns_exists ]; then 
   kubectl create ns mattermost-lt
fi
(
cd helm/loadtest
helm upgrade --install loadtest . -f values-${flavor}.yaml
)

# Once the mmloadtest pod is ready:
# kubectl port-forward mmloadtest 4000:4000 &
# curl -d "{\"LoadTestConfig\": $(cat config/config.json)}" http://localhost:4000/loadagent/create\?id\=lt0
# curl -X POST http://localhost:4000/loadagent/lt0/run
# curl -X POST http://localhost:4000/loadagent/lt0/addusers?amount=10
# curl localhost:4000/metrics
# curl -X POST http://localhost:4000/loadagent/lt0/removeusers?amount=10
# curl -X POST http://localhost:4000/loadagent/lt0/stop

