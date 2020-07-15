# gcp-auth-webhook
A mutating webhook that will patch any pods in your kubernetes cluster with GCP credentials (whose location is currently hard /var/lib/minikube/google_application_credentials.json)

Use the image gcr.io/k8s-minikube/gcp-auth-webhook as the image for a Deployment in your Kubernetes manifest and add that to a MutatingWebhookConfiguration.
