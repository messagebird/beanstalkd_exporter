#!/usr/bin/env bash
# a hack to generate releases like other prometheus projects
# use like this:
#       VERSION=1.0.1 ./release.sh


make release_linux
rm -rf "bin/beanstalkd_exporter-$VERSION.linux-amd64"
mkdir "bin/beanstalkd_exporter-$VERSION.linux-amd64"
cp bin/beanstalkd_exporter "bin/beanstalkd_exporter-$VERSION.linux-amd64/beanstalkd_exporter"
cd bin
tar -zcvf "beanstalkd_exporter-$VERSION.linux-amd64.tar.gz" "beanstalkd_exporter-$VERSION.linux-amd64"