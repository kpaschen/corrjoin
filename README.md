# Correlating Prometheus timeseries

## Getting started

Run the unit tests:

`go test ./...`

Build the docker container:

`docker build -f Dockerfile-receiver -t receiver:v0.1 .`

Bring up the system in a local kind cluster:

`(cd setup && ./setup.sh)`

If this fails:
- make sure `kind` is on your path
- login to ghcr.io like this:

 `echo $tok | docker login ghcr.io -u $your_github_username --password-stdin`

where `tok` is a github access token granting at least read access to public repositories.


