#!/bin/sh

# Flavor determines the values files to use for helm. Use local for a local (test) cluster,
# cloud for a "real" cluster. For 'cloud', take a look at the values-cloud.yaml files in the
# helm charts to make sure the values are good for you.
flavor=${FLAVOR:-local}

# We build a local apache image and an image for the mattermost loadtesting tool.
# These need to be placed in an image registry.
# This needs to match the image.registry setting in your values-${flavor}.yaml files.
imageregistry=${IMAGEREGISTRY:-localhost:5001}

# Set BUILDTESTHOME to a nonempty value if you want to build the custom apache
# and load test images. If you do this, they will be pushed to $imageregistry.

# Building the mattermost loadtesting tool involves checking it out from git.
# LTHOME is the parent directory of where you want to check out the loadtesting code.
# You only need this if you also set BUILDTESTHOME.
LTHOME=${LTHOME:-/home/developer/code}

repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "mattermost") .url')
if [ -z $repo_exists ]; then
   helm repo add mattermost https://helm.mattermost.com
fi
helm repo update

# Install postgres so mattermost can use it
kubectl get namespace -o name postgres
if [ $? -ne 0 ]; then 
   kubectl create ns postgres
fi

# The postgres chart also contains the mattermost db secret, so make sure
# that namespace also exists.
kubectl get namespace -o name mattermost
if [ $? -ne 0 ]; then 
   kubectl create ns mattermost
fi
(
cd helm/postgres &&
helm upgrade --install postgres . -f values-${flavor}.yaml
)

# Install mattermost
kubectl get namespace -o name mattermost-operator
if [ $? -ne 0 ]; then 
   kubectl create ns mattermost-operator
fi
helm upgrade --install mattermost-operator mattermost/mattermost-operator -n mattermost-operator -f mattermost-config.yaml

kubectl apply -f mattermost-installation.yaml

if [ ! -z ${BUILDPACKAGES} ]; then
echo "building custom apache image"
(
cd apache
docker build -f Dockerfile -t ${imageregistry}/mmapache:latest .
docker push ${imageregistry}/mmapache:latest
)
fi

# Set up apache in kubernetes.
# This is so we can attach an apache exporter to it for monitoring.
kubectl get namespace -o name apache
if [ $? -ne 0 ]; then 
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
kubectl -n mattermost exec $mmpod -c mattermost -- /mattermost/bin/mmctl --local roles system-admin mmadmin

if [ ! -z "${BUILDPACKAGES}" ]; then
echo "building custom apache mattermost load test image"
mkdir -p $LTHOME
(
cd $LTHOME
if [ ! -d mattermost-load-test-ng ]; then
  git clone https://github.com/mattermost/mattermost-load-test-ng
fi
cd mattermost-load-test-ng
git pull
)
echo $(pwd)
cp loadtest/Dockerfile ${LTHOME}/mattermost-load-test-ng/Dockerfile-lt
cp loadtest/config.json ${LTHOME}/mattermost-load-test-ng/config
cp loadtest/simplecontroller.json ${LTHOME}/mattermost-load-test-ng/config
(
cd ${LTHOME}/mattermost-load-test-ng
docker build -f Dockerfile-lt -t ${imageregistry}/mmloadtest:latest .
docker push ${imageregistry}/mmloadtest:latest
)
fi

kubectl get namespace -o name mattermost-lt
if [ $? -ne 0 ]; then 
   kubectl create ns mattermost-lt
fi
(
cd helm/loadtest
helm upgrade --install loadtest . -f values-${flavor}.yaml
)

echo "Now you can start the mattermost loadtest with './start-loadtest.sh'"


