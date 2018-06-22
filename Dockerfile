FROM alpine:3.7

ENV GOPATH=/go

WORKDIR /go/src/github.com/utilitywarehouse/kube-node-cycle-operator
COPY . /go/src/github.com/utilitywarehouse/kube-node-cycle-operator

# Fetch necessary tools, libs and run tests
RUN apk --no-cache add ca-certificates git go musl-dev && \
  go get -t ./... && \
  go test ./...

# Build agent
RUN \
 cd /go/src/github.com/utilitywarehouse/kube-node-cycle-operator/cmd/agent && \
 go get -t ./... && \
 CGO_ENABLED=0 go build -ldflags '-s -extldflags "-static"' -o /usr/bin/agent .

# Build operator
RUN \
 cd /go/src/github.com/utilitywarehouse/kube-node-cycle-operator/cmd/operator && \
 CGO_ENABLED=0 go build -ldflags '-s -extldflags "-static"' -o /usr/bin/operator .

# Clean
RUN apk del go musl-dev && rm -r /go
