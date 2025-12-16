# Build the manager binary

FROM golang:1.24 AS builder
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
ENV GOPROXY=http://mirrors.sangfor.org/nexus/repository/go-proxy
#RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY config/sql/nacos-pg.sql config/sql/nacos-pg.sql
COPY config/sql/nacos-mysql.sql config/sql/nacos-mysql.sql
# ADD https://raw.githubusercontent.com/alibaba/nacos/develop/distribution/conf/mysql-schema.sql config/sql/nacos-mysql.sql

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GO111MODULE=on go build  -a -v -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM docker.sangfor.com/paas-docker-base/alpine:3.17.3
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/config/sql/nacos-mysql.sql config/sql/nacos-mysql.sql
COPY --from=builder /workspace/config/sql/nacos-pg.sql config/sql/nacos-pg.sql

ENTRYPOINT ["/manager"]
