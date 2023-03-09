REGISTRY?=gcr.io/k8s-minikube
VERSION=v0.0.14
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
ARCH=$(if $(findstring amd64, $(GOARCH)),x86_64,$(GOARCH))
KO_VERSION=0.12.0
BASE_IMAGE?=gcr.io/distroless/static:nonroot

build: ## Build the gcp-auth-webhook binary
	CGO_ENABLED=0 GOOS=linux go build -ldflags="-X 'main.Version=$(VERSION)'" -o out/gcp-auth-webhook server.go

.PHONY: image
image: ## Create and push multiarch manifest and images
	@read -p "This will build and push $(REGISTRY)/gcp-auth-webhook:$(VERSION). Do you want to proceed? (Y/N): " confirm && echo $$confirm | grep -iq "^[yY]" || exit 1;
	curl -L https://github.com/google/ko/releases/download/v$(KO_VERSION)/ko_$(KO_VERSION)_$(GOOS)_$(ARCH).tar.gz | tar xzf - ko && chmod +x ./ko
	GOFLAGS="-ldflags=-X=main.Version=$(VERSION)" KO_DOCKER_REPO=$(REGISTRY) KO_DEFAULTBASEIMAGE=$(BASE_IMAGE) ./ko publish -B . --platform all -t $(VERSION)
	rm ./ko

.PHONY: local-image
local-image: build
	docker build -t local/gcp-auth-webhook:$(VERSION) -f Dockerfile ./out
