FROM golang:1.22 as build

WORKDIR /src
COPY . .

RUN go mod download
RUN go vet -v ./...
RUN go test -v ./...

RUN CGO_ENABLED=0 go build -C prometheus -o receiver

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /src/prometheus/receiver /
USER 65532:65532
CMD ["/receiver"]

