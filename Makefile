all: test

ifdef CI
CI_GINKGO_ARGS=--v -compilers 4
else
CI_GINKGO_ARGS=
endif

# Run tests
test: fmt vet
	ginkgo --randomizeAllSpecs --randomizeSuites --cover --trace --progress ${GINKGO_ARGS} ${CI_GINKGO_ARGS} -r ./...
	gover

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Display a coverage report
cover:
	go tool cover -html=gover.coverprofile
