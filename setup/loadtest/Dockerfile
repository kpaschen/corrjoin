FROM golang:1.23-alpine

ENV GOPROXY="https://proxy.golang.org,direct"
ENV GOBIN=/usr/local/bin

RUN apk add --no-cache build-base bash make

WORKDIR /loadtest
COPY . .

RUN make assets
RUN make build-linux

CMD ["go run cmd/ltagent init && go run ./cmd/ltagent -n 30 -d 600"]

