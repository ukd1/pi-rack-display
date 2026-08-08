# Pi Rack Display

`pi-rack-display` turns the 160x80 displays in a UCTRONICS Pi Rack Pro into
Kubernetes node dashboards. One small Go binary contains both the display
daemon and a kubelet device plugin, and GitHub Actions publishes the arm64
container to `ghcr.io/ukd1/pi-rack-display`.

Every page has the same status header: the local node name and state on the
left, and the number of Ready Kubernetes nodes on the right (`NODES 4/4`). The
body rotates through two deliberately distinct views:

- active pod count and any unready pods on this node; and
- CPU, memory, and SoC temperature.

The node-state square is green when the node is Ready and schedulable, yellow
when its state is unknown or it is cordoned, and red when it is not Ready. A
yellow `C` beside the node name also identifies a cordoned node. Cluster
readiness appears only in the header; the pod view uses a count and state
instead of another fraction.

## Architecture

The device-plugin DaemonSet advertises each rack display as the extended
resource `uctronics.com/rm0004`. It gives the consuming display pod exclusive
access to `/dev/i2c-1` and injects the thermal sensor as a read-only file. The
display pod itself runs as UID/GID 65532, has no Linux capabilities, uses a
read-only root filesystem, and runs in a namespace enforcing the Restricted
Pod Security Standard.

The service polls only the Kubernetes resources it displays. Its ClusterRole
can get/list nodes, list pods, and get node metrics; it cannot read Secrets or
write cluster objects.

## Requirements

- UCTRONICS Pi Rack Pro RM0004 display bridge at I2C address `0x18`;
- Linux I2C device `/dev/i2c-1`;
- Raspberry Pi thermal sensor at
  `/sys/class/thermal/thermal_zone0/temp`;
- Kubernetes metrics-server for CPU and memory pages; and
- arm64 Kubernetes nodes.

Enable I2C in the Pi boot configuration:

```text
dtparam=i2c_arm=on
```

UCTRONICS recommends a 400 kHz bus with
`dtparam=i2c_arm=on,i2c_arm_baudrate=400000`. The daemon also works at the
default 100 kHz and sends only the smallest changed screen rectangle. The
faster clock gives the rotating dashboard more transfer headroom.

## Deploy

The checked-in Kustomization is pinned to the image digest used by the running
cluster. Opt nodes in with a hardware inventory label, then apply it:

```sh
kubectl label node your-node uctronics-rm0004=true --overwrite
kubectl apply -k deploy
kubectl -n kube-system rollout status ds/uctronics-rm0004-device-plugin
kubectl -n pi-rack-display rollout status ds/pi-rack-display
```

For a canary rollout, label one node first. After its display pod reports Ready
and logs `first display frame written`, label the remaining rack nodes. A
DaemonSet automatically tolerates cordoned nodes, and the manifests also
tolerate ordinary `NoSchedule` and `NoExecute` taints.

No image pull secret is needed because the GHCR package is public.

## Development

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/pi-rack-display
go run ./cmd/pi-rack-display-preview -output /tmp/pi-rack-display-preview
kubectl kustomize deploy | kubectl create --dry-run=client --validate=false -f -
```

The preview command writes deterministic healthy and degraded 160x80 PNGs for
both pages so layout changes can be inspected without display hardware.

The Linux display driver is transport-injected and tested without hardware.
It sends RGB565 pixels through the rack's I2C bridge in at-most-160-byte bursts,
as required by the bridge protocol.

## Compatibility note

The RM0004 protocol support is an independent compatibility implementation
based on observable register and framing behavior in UCTRONICS' public
[`SKU_RM0004`](https://github.com/UCTRONICS/SKU_RM0004) repository. No vendor
source code, fonts, or screen design are included here; that repository did not
show a license when this implementation was created.

## License

MIT
