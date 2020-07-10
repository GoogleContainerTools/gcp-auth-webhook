REGISTRY?=gcr.io/k8s-minikube

build:
	CGO_ENABLED=0 GOOS=linux go build -o out/gcp-auth-webhook -ldflags=$(PROVISIONER_LDFLAGS) server.go

.PHONY: image
image: build
	docker build -t $(REGISTRY)/gcp-auth-webhook -f Dockerfile ./out


.PHONY: push
push: image ## Push metadata-server docker image using gcloud
	gcloud docker -- push $(REGISTRY)/gcp-auth-webhook

