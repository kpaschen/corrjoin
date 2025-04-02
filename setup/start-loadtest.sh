#!/bin/sh

# You need to have a post called mmloadtest already running.
# install-loadtestenv.sh does that.
# Modify the number of users to create more load.

# The loadtest will run until you execute stop-loadtest.sh

nohup kubectl port-forward mmloadtest 4000:4000 &
fwdpid=$!

trap '{
   kill $fwdpid
}' EXIT

while ! nc -vz localhost 4000 > /dev/null 2>&1 ; do
   sleep 0.1
done


curl -d "{\"LoadTestConfig\": $(cat loadtest/config.json)}" http://localhost:4000/loadagent/create\?id\=lt0
curl -X POST http://localhost:4000/loadagent/lt0/run
curl -X POST http://localhost:4000/loadagent/lt0/addusers?amount=200

echo "Loadtest is running. stop-loadtest.sh will stop it."
