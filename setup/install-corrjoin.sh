#!/bin/sh

# Install the correlation join system and a kube-prometheus operator.
# If you already have prometheus, you can just install the corrjoin chart,
# but you will need to add the remoteWrite spec to your prometheus.
# You can find the remoteWrite spec in prometheus-values.yaml.

# flavor determines the values file we use for helm.
# The 'local' flavor uses localvolumes and is probably most useful for kind clusters.
# The 'cloud' flavor can be used with a Kubernetes cluster in the cloud, but you
# probably want to go over the values files and adapt them.
# There should be a file called "values-${flavor}.yaml" for every helm chart.
flavor=${FLAVOR:-local}

# Add helm repository for prometheus.
repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "prometheus-community") .url')
if [ -z $repo_exists ]; then
   helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
fi
helm repo update

# Install the correlation join system
(
cd helm/corrjoin &&
helm upgrade --install corrjoin . -f values-${flavor}.yaml
)

# Start kube-prometheus operator with local values file.
kubectl get namespace -o name monitoring 2>/dev/null
if [ $? -ne 0 ]; then 
   kubectl create ns monitoring
fi
helm upgrade --install prometheus prometheus-community/kube-prometheus-stack -f prometheus-values.yaml

# Install receiver service monitor after the prometheus stack crds are there.
kubectl apply -f receiver-service-monitor.yaml
kubectl apply -f lv-podmonitor.yaml
