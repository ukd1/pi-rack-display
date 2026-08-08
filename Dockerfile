# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS build

ARG TARGETOS=linux
ARG TARGETARCH=arm64
ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
    -o /out/pi-rack-display ./cmd/pi-rack-display

FROM scratch
COPY --from=build /out/pi-rack-display /pi-rack-display
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/pi-rack-display"]
CMD ["display"]
