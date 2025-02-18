#!/bin/sh

docker stop traefik 2>/dev/null
docker rm traefik 2>/dev/null

cwd="$(dirname $0)"

mountLetsEncrypt="type=bind,source=$cwd/letsencrypt,target=/letsencrypt"
mountConfig="type=bind,source=$cwd/config,target=/etc/traefik/config,readonly"
mountTraefik="type=bind,source=$cwd/traefik.yml,target=/etc/traefik/traefik.yml,readonly"

docker run -d -p 80:80 -p 443:443 -p 8080:8080 \
  --mount $mountLetsEncrypt --mount $mountConfig --mount $mountTraefik --name traefik \
  traefik:v3.2 --configfile=/etc/traefik/traefik.yml

