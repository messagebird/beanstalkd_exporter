BINARY  = beanstalkd_exporter
PROJECT = beanstalkd_exporter

VERSION = $(shell git rev-list HEAD | wc -l |tr -d ' ')
HASH    = $(shell git rev-parse --short HEAD)

GO      = env GOPATH="$(PWD)/vendor:$(PWD)" go
LDFLAGS = -X main.BuildNumber=$(VERSION) -X main.CommitHash=$(HASH)

all:
	@echo "make release       # Build $(PROJECT) for release"
	@echo "make development   # Build $(PROJECT) for development"
	@echo
	@echo "make run           # Run a development version of $(BINARY)"
	@echo
	@echo "make test          # Run the test suite"
	@echo "make clean         # Clean up the project directory"

release_linux: export GOOS=linux
release_linux: export GOARCH=amd64
release_linux: release
	@mv bin/linux_amd64/${BINARY} bin/

release: clean
	@echo "* Building $(PROJECT) for release"
	@$(GO) install -ldflags '$(LDFLAGS)' $(PROJECT)/...

development: clean
	@echo "* Building $(PROJECT) for development"
	@$(GO) install -ldflags '$(LDFLAGS)' -race $(PROJECT)/...

dependencies:
	@echo "* go getting all dependencies into vendor/"
	@$(GO) get -t $(PROJECT)/...
	find vendor/ -name .git -type d | xargs rm -rf

run: development
	@echo "* Running development $(PROJECT) binary"
	@./bin/$(BINARY) -mapping-config=./examples/mapping.conf -log.level=debug

test:
	@echo "* Running tests"
	@$(GO) test $(PROJECT)/...
	@echo

clean:
	rm -fr bin pkg vendor/pkg lib/*.a lib/*.o
