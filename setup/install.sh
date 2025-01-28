#!/bin/sh

# Set up rook-ceph
helm repo add rook-release https://charts.rook.io/release
helm install --create-namespace --namespace rook-ceph rook-ceph rook-release/rook-ceph -f rook-values.yaml

# You need to have created a volume before running this.
# The ceph-values.yaml file here will only use a volume /dev/sdb on a node called 'node4'
helm install --create-namespace --namespace rook-ceph rook-ceph-cluster rook-release/rook-ceph-cluster -f ceph-values.yaml

# Start kube-prometheus operator with local values file.
repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "prometheus-community") .url')
if [ -z $repo_exists ]; then
   helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
fi
helm repo update
ns_exists=$(kubectl get namespace -o name | grep monitoring)
if [ -z $ns_exists ]; then 
   kubectl create ns monitoring
fi
helm upgrade --install prometheus prometheus-community/kube-prometheus-stack -f prometheus-values.yaml

kubectl apply -f receiver/receiver-deployment.yaml
kubectl apply -f receiver/receiver-results-pvc-ceph.yaml
kubectl apply -f receiver/receiver-svc.yaml
kubectl apply -f receiver/receiver-service-monitor.yaml



