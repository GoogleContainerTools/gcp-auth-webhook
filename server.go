/*Copyright 2020 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.*/

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const gcpAuth = "gcp-auth"

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

var projectAliases = []string{
	"PROJECT_ID",
	"GCP_PROJECT",
	"GCLOUD_PROJECT",
	"GOOGLE_CLOUD_PROJECT",
	"CLOUDSDK_CORE_PROJECT",
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// mutateHandler mounts in the volumes and adds the appropriate env vars to new pods
func mutateHandler(w http.ResponseWriter, r *http.Request) {
	ar := getAdmissionReview(w, r)

	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		log.Printf("Could not unmarshal raw object: %v", err)
		writeError(w, &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		})
		return
	}

	var patch []patchOperation
	var envVars []corev1.EnvVar

	needsCreds := needsEnvVar(pod.Spec.Containers[0], "GOOGLE_APPLICATION_CREDENTIALS")

	// Explicitly and silently exclude the kube-system namespace
	if pod.ObjectMeta.Namespace != metav1.NamespaceSystem {
		// Define the volume to mount in
		v := corev1.Volume{
			Name: "gcp-creds",
			VolumeSource: corev1.VolumeSource{
				HostPath: func() *corev1.HostPathVolumeSource {
					h := corev1.HostPathVolumeSource{
						Path: "/var/lib/minikube/google_application_credentials.json",
						Type: func() *corev1.HostPathType {
							hpt := corev1.HostPathFile
							return &hpt
						}(),
					}
					return &h
				}(),
			},
		}

		// Mount the volume in
		mount := corev1.VolumeMount{
			Name:      "gcp-creds",
			MountPath: "/google-app-creds.json",
			ReadOnly:  true,
		}

		if needsCreds {
			// Define the env var
			e := corev1.EnvVar{
				Name:  "GOOGLE_APPLICATION_CREDENTIALS",
				Value: "/google-app-creds.json",
			}
			envVars = append(envVars, e)

			// add the volume in the list of patches
			addVolume := true
			for _, vl := range pod.Spec.Volumes {
				if vl.Name == v.Name {
					addVolume = false
					break
				}
			}
			if addVolume {
				patch = append(patch, patchOperation{
					Op:    "add",
					Path:  "/spec/volumes",
					Value: append(pod.Spec.Volumes, v),
				})
			}
		}

		// If GOOGLE_CLOUD_PROJECT is set in the VM, set it for all GCP apps.
		if _, err := os.Stat("/var/lib/minikube/google_cloud_project"); err == nil {
			project, err := ioutil.ReadFile("/var/lib/minikube/google_cloud_project")
			if err == nil {
				// Set the project name for every variant of the project env var
				for _, a := range projectAliases {
					if needsEnvVar(pod.Spec.Containers[0], a) {
						envVars = append(envVars, corev1.EnvVar{
							Name:  a,
							Value: string(project),
						})
					}
				}
			}
		}

		if len(envVars) > 0 {
			for i, c := range pod.Spec.Containers {
				if needsCreds {
					if len(c.VolumeMounts) == 0 {
						patch = append(patch, patchOperation{
							Op:    "add",
							Path:  fmt.Sprintf("/spec/containers/%d/volumeMounts", i),
							Value: []corev1.VolumeMount{mount},
						})
					} else {
						addMount := true
						for _, vm := range c.VolumeMounts {
							if vm.Name == mount.Name {
								addMount = false
								break
							}
						}
						if addMount {
							patch = append(patch, patchOperation{
								Op:    "add",
								Path:  fmt.Sprintf("/spec/containers/%d/volumeMounts", i),
								Value: append(c.VolumeMounts, mount),
							})
						}
					}
				}
				if len(c.Env) == 0 {
					patch = append(patch, patchOperation{
						Op:    "add",
						Path:  fmt.Sprintf("/spec/containers/%d/env", i),
						Value: envVars,
					})
				} else {
					patch = append(patch, patchOperation{
						Op:    "add",
						Path:  fmt.Sprintf("/spec/containers/%d/env", i),
						Value: append(c.Env, envVars...),
					})
				}
			}
		}
	}

	writePatch(w, ar, patch)
}

// serviceaccountHandler adds image pull secret to new service accounts
func serviceaccountHandler(w http.ResponseWriter, r *http.Request) {
	ar := getAdmissionReview(w, r)

	req := ar.Request
	var sa corev1.ServiceAccount
	if err := json.Unmarshal(req.Object.Raw, &sa); err != nil {
		log.Printf("Could not unmarshal raw object: %v", err)
		writeError(w, &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		})
		return
	}

	// Make sure the gcp-auth secret exists before adding it as a pull secret
	hasSecret := false
	for _, s := range sa.Secrets {
		if s.Name == gcpAuth {
			hasSecret = true
			break
		}
	}

	var patch []patchOperation

	if hasSecret {
		ips := corev1.LocalObjectReference{Name: gcpAuth}
		if len(sa.ImagePullSecrets) == 0 {
			patch = []patchOperation{{
				Op:    "add",
				Path:  "/imagePullSecrets",
				Value: []corev1.LocalObjectReference{ips},
			}}
		} else {
			patch = []patchOperation{{
				Op:    "add",
				Path:  "/imagePullSecrets",
				Value: append(sa.ImagePullSecrets, ips),
			}}
		}
	}

	writePatch(w, ar, patch)
}

// getAdmissionReview reads and validates an inbound request and returns an admissionReview
func getAdmissionReview(w http.ResponseWriter, r *http.Request) *admissionv1.AdmissionReview {
	log.Printf("%v\n", r)

	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	if len(body) == 0 {
		log.Print("request body was empty, returning")
		http.Error(w, "empty body", http.StatusBadRequest)
		return nil
	}

	ar := admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		log.Printf("Can't decode body: %v", err)
		writeError(w, &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		})
		return nil
	}
	return &ar
}

// writeError writes an error response
func writeError(w http.ResponseWriter, admissionResp *admissionv1.AdmissionResponse) {
	admissionReview := admissionv1.AdmissionReview{
		Response: admissionResp,
	}
	writeResp(w, admissionReview)
}

// writePatch writes a patch response
func writePatch(w http.ResponseWriter, ar *admissionv1.AdmissionReview, patch []patchOperation) {
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		writeError(w, &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		})
		return
	}

	admissionResp := &admissionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}

	admissionReview := admissionv1.AdmissionReview{
		Response: admissionResp,
	}
	if ar.Request != nil {
		admissionReview.Response.UID = ar.Request.UID
	}

	writeResp(w, admissionReview)
}

// writeResp writes an admissionReview response
func writeResp(w http.ResponseWriter, admissionReview admissionv1.AdmissionReview) {
	admissionReview.Kind = "AdmissionReview"
	admissionReview.APIVersion = "admission.k8s.io/v1"

	log.Printf("Ready to marshal response ...")
	resp, err := json.Marshal(admissionReview)
	if err != nil {
		log.Printf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	log.Printf("Ready to write response ...")
	if _, err := w.Write(resp); err != nil {
		log.Printf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

func needsEnvVar(c corev1.Container, name string) bool {
	for _, e := range c.Env {
		if e.Name == name {
			return false
		}
	}
	return true
}

func main() {
	log.Print("GCP Auth Webhook started!")

	mux := http.NewServeMux()

	mux.HandleFunc("/mutate", mutateHandler)
	mux.HandleFunc("/mutate/sa", serviceaccountHandler)

	s := &http.Server{
		Addr:    ":8443",
		Handler: mux,
	}

	log.Fatal(s.ListenAndServeTLS("/etc/webhook/certs/cert", "/etc/webhook/certs/key"))
}
