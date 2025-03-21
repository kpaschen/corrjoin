#!/bin/sh

# This stops a running loadtest on localhost:4000.

nohup kubectl port-forward mmloadtest 4000:4000 &
fwdpid=$!

trap '{
  kill $fwdpid
}' EXIT

while ! nc -vz localhost 4000 > /dev/null 2>&1 ; do
  sleep 0.1
done

curl -X POST http://localhost:4000/loadagent/lt0/removeusers?amount=10
curl -X POST http://localhost:4000/loadagent/lt0/stop

echo "loadtest stopped"
