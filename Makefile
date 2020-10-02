REGISTRY?=gcr.io/k8s-minikube
VERSION=v0.0.3-snapshot

build:
	CGO_ENABLED=0 GOOS=linux go build -o out/gcp-auth-webhook -ldflags=$(PROVISIONER_LDFLAGS) server.go

.PHONY: image
image: build
	docker build -t $(REGISTRY)/gcp-auth-webhook:$(VERSION) -f Dockerfile ./out


.PHONY: push
push: image
	docker push $(REGISTRY)/gcp-auth-webhook:$(VERSION)

