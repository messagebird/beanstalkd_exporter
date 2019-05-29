FROM golang:alpine as build-env
# All these steps will be cached

RUN apk add git
RUN mkdir /beanstalkd_exporter
WORKDIR /beanstalkd_exporter
COPY go.mod .
COPY go.sum .

# Get dependencies - will also be cached if we won't change mod/sum
RUN go mod download
# COPY the source code as the last step
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags "-s -w" -o /go/bin/beanstalkd_exporter .

# <- Second step to build minimal image
FROM scratch
COPY examples/ /etc/beanstalkd_exporter/
COPY --from=build-env /go/bin/beanstalkd_exporter /go/bin/beanstalkd_exporter
ENTRYPOINT ["/go/bin/beanstalkd_exporter"]
CMD ["-beanstalkd.address", "beanstalkd:11300", "-mapping-config", "/etc/beanstalkd_exporter/mapping.conf"]
