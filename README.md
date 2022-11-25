# gcp-auth-webhook

A server that includes:
* A mutating webhook that will patch any newly created pods in your kubernetes cluster with GCP credentials (whose location is currently hardcoded to /var/lib/minikube/google_application_credentials.json).
* A mutating webhook that will patch any newly created service accounts in your kubernetes cluster with an image pull secret.
* A thread that monitors namespaces to make sure all namespaces include a image pull secret to be able to pull from GCR and AR.

## Deployment
Use the image gcr.io/k8s-minikube/gcp-auth-webhook as the image for a Deployment in your Kubernetes manifest and add that to a MutatingWebhookConfiguration. See [minikube](https://github.com/kubernetes/minikube/blob/master/deploy/addons/gcp-auth/gcp-auth-webhook.yaml.tmpl) for details.

## Running Locally
The easiest way to run the server locally is:
* Remove `FROM scratch` in the Dockerfile and replace it with the following to ensure https requests work properly locally:
```
FROM alpine
RUN apk --no-cache add ca-certificates
```
* Modify [minikube's](https://github.com/kubernetes/minikube/blob/master/deploy/addons/gcp-auth/gcp-auth-webhook.yaml.tmpl) gcp-auth Deployment image to be `local/gcp-auth-webhook:$(VERSION)` (replace `$(VERSION)` with your version)
* Build and run minikube
* Run `eval $(path_to_minikube/minikube docker-env)` and then `make local-image` to make the image available from within minikube
* Run `path_to_minikube/minikube addons enable gcp-auth` to enable the addon, which creates a pod in the `gcp-auth` namespace with the gcp-auth-webhook server