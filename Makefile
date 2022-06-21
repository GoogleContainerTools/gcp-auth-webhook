REGISTRY?=gcr.io/k8s-minikube
VERSION=v0.0.9
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
KO_VERSION=0.11.2

build: ## Build the gcp-auth-webhook binary
	CGO_ENABLED=0 GOOS=linux go build -o out/gcp-auth-webhook server.go

.PHONY: image
image: ## Create and push multiarch manifest and images
	@read -p "This will build and push $(REGISTRY)/gcp-auth-webhook:$(VERSION). Do you want to proceed? (Y/N): " confirm && echo $$confirm | grep -iq "^[yY]" || exit 1;
	curl -L https://github.com/google/ko/releases/download/v$(KO_VERSION)/ko_$(KO_VERSION)_$(GOOS)_$(GOARCH).tar.gz | tar xzf - ko && chmod +x ./ko
	KO_DOCKER_REPO=$(REGISTRY) ./ko publish -B . --platform all -t $(VERSION)
	rm ./ko

.PHONY: local-image
local-image: build
	docker build -t local/gcp-auth-webhook:$(VERSION) -f Dockerfile ./out
