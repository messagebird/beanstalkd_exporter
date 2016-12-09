FROM golang:alpine

RUN apk add --no-cache --virtual git && \
    go-wrapper download github.com/messagebird/beanstalkd_exporter/src/beanstalkd_exporter/... && \
    cd $GOPATH/src/github.com/messagebird/beanstalkd_exporter && \
    env GOPATH="$PWD/vendor:$PWD" go-wrapper install beanstalkd_exporter/... && \
    cp -v bin/beanstalkd_exporter /usr/local/bin/beanstalkd_exporter && \
    rm -rvf $GOPATH && \
    apk del git

COPY examples/ /etc/beanstalkd_exporter/

EXPOSE 8080
ENTRYPOINT ["beanstalkd_exporter"]
CMD ["-config", "/etc/beanstalkd_exporter/servers.conf", "-mapping-config", "/etc/beanstalkd_exporter/mapping.conf"]