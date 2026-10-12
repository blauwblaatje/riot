# RATS in a container: a static Go binary on distroless, as a non-root user.
# Build: docker build -t rats .
# Multi-arch (amd64, arm64 for a Raspberry Pi k3s node): make image-multi
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=vendor -trimpath \
      -ldflags "-s -w -X main.version=$VERSION" -o /out/rats ./cmd/rats
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/rats /rats
COPY --from=build --chown=65532:65532 /out/data /data
ENV RATS_ADDR=:4201 RATS_DATA=/data RATS_LOGS=/data/logs
EXPOSE 4201
VOLUME /data
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s CMD ["/rats", "-health"]
ENTRYPOINT ["/rats"]
