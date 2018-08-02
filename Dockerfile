FROM golang:1.9-alpine3.7

RUN apk add --no-cache --virtual git && \
    go-wrapper download github.com/messagebird/beanstalkd_exporter && \
    go-wrapper install github.com/messagebird/beanstalkd_exporter && \
    cp -v $GOPATH/bin/beanstalkd_exporter /usr/local/bin/beanstalkd_exporter && \
    rm -rvf $GOPATH && \
    apk del git

COPY examples/ /etc/beanstalkd_exporter/

EXPOSE 8080
ENTRYPOINT ["beanstalkd_exporter"]
CMD ["-beanstalkd.address", "beanstalkd:11300", "-mapping-config", "/etc/beanstalkd_exporter/mapping.conf"]
