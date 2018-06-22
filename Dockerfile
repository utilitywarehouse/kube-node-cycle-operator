FROM alpine:3.7

ENV GOPATH=/go

WORKDIR /go/src/app
COPY . /go/src/app/

RUN apk --no-cache add ca-certificates git go musl-dev && \
  go get ./... && \
  go test ./... && \
  (cd cmd/operator && CGO_ENABLED=0 go build -ldflags '-s -extldflags "-static"' -o /kube-node-cycle-operator .) && \
  (cd cmd/agent && CGO_ENABLED=0 go build -ldflags '-s -extldflags "-static"' -o /kube-node-cycle-agent .) && \
  apk del go git musl-dev && \
  rm -rf $GOPATH
