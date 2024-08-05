ARG ARCH=amd64
#FROM gcr.io/distroless/static:nonroot-${ARCH}
FROM ubuntu:jammy
WORKDIR /bin
COPY receiver /bin/receiver
USER 65532:65532

ENTRYPOINT ["/bin/receiver"]
