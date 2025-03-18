#!/bin/sh

# Make sure the directories for localstorage exist
mkdir -p /tmp/corrjoinResults
mkdir -p /tmp/postgres

# 1. Create registry container unless it already exists
reg_name='kind-registry'
reg_port='5001'
if [ "$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)" != 'true' ]; then
  docker run \
    -d --restart=always -p "127.0.0.1:${reg_port}:5000" --network bridge --name "${reg_name}" \
    registry:2
fi

# 2. Create kind cluster with containerd registry config dir enabled
# TODO: kind will eventually enable this by default and this patch will
# be unnecessary.
#
# See:
# https://github.com/kubernetes-sigs/kind/issues/2875
# https://github.com/containerd/containerd/blob/main/docs/cri/config.md#registry-configuration
# See: https://github.com/containerd/containerd/blob/main/docs/hosts.md
cluster=$(kind get clusters | grep kind)
if [ -z $cluster ]; then
   kind create cluster --config=cluster-config.yaml
fi

# 3. Add the registry config to the nodes
#
# This is necessary because localhost resolves to loopback addresses that are
# network-namespace local.
# In other words: localhost in the container is not localhost on the host.
#
# We want a consistent name that works from both ends, so we tell containerd to
# alias localhost:${reg_port} to the registry container when pulling images
REGISTRY_DIR="/etc/containerd/certs.d/localhost:${reg_port}"
for node in $(kind get nodes); do
  docker exec "${node}" mkdir -p "${REGISTRY_DIR}"
  cat <<EOF | docker exec -i "${node}" cp /dev/stdin "${REGISTRY_DIR}/hosts.toml"
[host."http://${reg_name}:5000"]
EOF
done

# 4. Connect the registry to the cluster network if not already connected
# This allows kind to bootstrap the network but ensures they're on the same network
if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]; then
  docker network connect "kind" "${reg_name}"
fi

# 5. Document the local registry
# https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${reg_port}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

# Enable metrics server

repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "metrics-server") .url')
if [ -z $repo_exists ]; then
   helm repo add metrics-server https://kubernetes-sigs.github.io/metrics-server
fi
helm repo update
helm upgrade --install --set args={--kubelet-insecure-tls} metrics-server metrics-server/metrics-server --namespace kube-system

repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "prometheus-community") .url')
if [ -z $repo_exists ]; then
   helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
fi

(
cd helm/corrjoin &&
helm upgrade --install corrjoin . -f values-local.yaml
)

# Start kube-prometheus operator with local values file.
ns_exists=$(kubectl get namespace -o name | grep monitoring)
if [ -z $ns_exists ]; then 
   kubectl create ns monitoring
fi
helm repo update
helm upgrade --install prometheus prometheus-community/kube-prometheus-stack -f prometheus-values.yaml

# Install receiver service monitor after the prometheus stack crds are there.
kubectl apply -f receiver-service-monitor.yaml

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
helm upgrade --install postgres . -f values-local.yaml
)

# Login to postgres and create db and user
#kubectl -n postgres exec -it <postgres pod> -- /bin/sh
#psql ps_db ps_user
#create database mattermost_main;
#\connect mattermost_main;
#create user mmuser with password 'matter';
#grant all privileges on database mattermost to mmuser;
#grant usage, create on schema public to mmuser;
#\q

# Install mattermost
repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "mattermost") .url')
if [ -z $repo_exists ]; then
   helm repo add mattermost https://helm.mattermost.com
fi
helm repo update
ns_exists=$(kubectl get namespace -o name | grep mattermost-operator)
if [ -z $ns_exists ]; then 
   kubectl create ns mattermost-operator
fi
helm upgrade --install mattermost-operator mattermost/mattermost-operator -n mattermost-operator -f mattermost-config.yaml

kubectl apply -f mattermost-installation.yaml

# Set up apache in kubernetes.
# This is so we can attach an apache exporter to it for monitoring.
ns_exists=$(kubectl get namespace -o name | grep apache)
if [ -z $ns_exists ]; then 
   kubectl create ns apache
fi
(
cd helm/apache &&
helm upgrade --install apache . -f values-local.yaml
)

