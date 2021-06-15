REGISTRY?=gcr.io/k8s-minikube
VERSION=v0.0.6
GOOS?=$(shell go env GOOS)

build: ## Build the gcp-auth-webhook binary
	CGO_ENABLED=0 GOOS=linux go build -o out/gcp-auth-webhook server.go

.PHONY: image
image: ## Create and push multiarch manifest and images
	curl -L https://github.com/google/ko/releases/download/v0.8.0/ko_0.8.0_$(GOOS)_x86_64.tar.gz | tar xzf - ko && chmod +x ./ko
	KO_DOCKER_REPO=$(REGISTRY) ./ko publish -B . --platform all -t $(VERSION)
	rm ./ko

.PHONY: local-image
local-image: build
	docker build -t local/gcp-auth-webhook:$(VERSION) -f Dockerfile ./out
