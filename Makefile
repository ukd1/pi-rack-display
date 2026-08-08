.PHONY: test verify build validate-manifests

test:
	go test ./...

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/pi-rack-display

validate-manifests:
	kubectl kustomize deploy | kubectl create --dry-run=client --validate=false -f - >/dev/null

verify: test build validate-manifests
	go vet ./...
