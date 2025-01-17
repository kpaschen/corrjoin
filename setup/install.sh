#!/bin/sh

# enable nginx ingress
kubectl apply -f ingress-deploy.yaml

# Enable metrics server

repo_exists=$(helm repo list -o json | yq '.[] | select(.name == "metrics-server") .url')
if [ -z $repo_exists ]; then
   helm repo add metrics-server https://kubernetes-sigs.github.io/metrics-server
fi
helm repo update
helm upgrade --install --set args={--kubelet-insecure-tls} metrics-server metrics-server/metrics-server --namespace kube-system


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
kubectl apply -f receiver/receiver-results-pv.yaml
kubectl apply -f receiver/receiver-svc.yaml
kubectl apply -f receiver/receiver-service-monitor.yaml

kubectl -n ingress-nginx wait --for=condition=available deploy/ingress-nginx-controller --timeout=240s
kubectl apply -f receiver/ingress.yaml


