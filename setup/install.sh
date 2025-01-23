#!/bin/sh

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
kubectl apply -f receiver/receiver-results-pv-localstorage.yaml
kubectl apply -f receiver/receiver-svc.yaml
kubectl apply -f receiver/receiver-service-monitor.yaml



