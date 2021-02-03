REGISTRY?=gcr.io/k8s-minikube
VERSION=v0.0.3

build: ## Build the gcp-auth-webhook binary
	CGO_ENABLED=0 GOOS=linux go build -o out/gcp-auth-webhook -ldflags=$(PROVISIONER_LDFLAGS) server.go

.PHONY: image
image: build ## Create the multiarch manifest builder
	env DOCKER_CLI_EXPERIMENTAL=enabled docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
	env DOCKER_CLI_EXPERIMENTAL=enabled docker buildx rm --builder gcp-auth-builder || true
	env DOCKER_CLI_EXPERIMENTAL=enabled docker buildx create --name gcp-auth-builder --use


.PHONY: push
push: image ## Push the manifest and images up to the registry
	env DOCKER_CLI_EXPERIMENTAL=enabled docker buildx build --platform linux/arm64,linux/amd64 -t $(REGISTRY)/gcp-auth-webhook:$(VERSION) --push -f Dockerfile ./out

