#!/bin/sh

# This stops a running loadtest on localhost:4000.

kubectl port-forward mmloadtest 4000:4000 &

curl -X POST http://localhost:4000/loadagent/lt0/removeusers?amount=10
curl -X POST http://localhost:4000/loadagent/lt0/stop

echo "loadtest stopped"
