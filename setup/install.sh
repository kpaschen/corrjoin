#!/bin/sh

# Set up rook-ceph
# Skip this and set up local-storage instead if you are short on cpu.
# rook/ceph will request about 2 cores per node.
#helm repo add rook-release https://charts.rook.io/release
#helm install --create-namespace --namespace rook-ceph rook-ceph rook-release/rook-ceph -f rook/rook-values.yaml

# You need to have created a volume before running this.
# The ceph-values.yaml file here will only use a volume /dev/sdb on a node called 'node4'
#helm install --create-namespace --namespace rook-ceph rook-ceph-cluster rook-release/rook-ceph-cluster -f rook/ceph-values.yaml

# If using local-storage, need to pepare local storage volumes.
# On each node, either:
# mkdir /mnt/disks/$vol
# mount -t tmpfs -o size=5G $vol /mnt/disks/$vol
# or (if you have a volume mounted at /dev/sdb):
# mkdir /mnt/disks/ssd1
# mount /dev/sdb /mnt/disks/ssd1
# more reading: https://github.com/kubernetes-retired/external-storage/tree/master/local-volume

kubectl label node node2 resultsVolumeMounted=true
kubectl label node node3 postgresVolumeMounted=true

# There is a secret in this chart that has the grafana admin password, that's why I install
# this chart before the prometheus operator.
(
cd helm/corrjoin &&
helm upgrade --install corrjoin . -f values-cloud.yaml
)

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

kubectl apply -f receiver-service-monitor.yaml

# Set up traefik
tar czf traefik.tgz traefik
scp traefik.tgz ubuntu@$BASTION:/home/ubuntu

# Login to BASTION, unpack traefik.tgz and run start-traefik.sh
#  Make sure port 443 is open in the security rules for the bastion

# Install postgres (needed for Mattermost)
ns_exists=$(kubectl get namespace -o name | grep postgres)
if [ -z $ns_exists ]; then 
   kubectl create ns postgres
fi
(
cd helm/postgres &&
helm upgrade --install postgres . -f values-cloud.yaml
)
