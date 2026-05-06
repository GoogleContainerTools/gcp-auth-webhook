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
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSkipNamespace(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"kube-system", true},
		{"gcp-auth", true},
		{"default", false},
		{"kube-public", false},
		{"kube-node-lease", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := skipNamespace(test.name)
			if actual != test.expected {
				t.Errorf("skipNamespace(%q) = %v; want %v", test.name, actual, test.expected)
			}
		})
	}
}

func TestNeedsEnvVar(t *testing.T) {
	tests := []struct {
		name      string
		container corev1.Container
		envName   string
		expected  bool
	}{
		{
			name: "empty env",
			container: corev1.Container{
				Env: []corev1.EnvVar{},
			},
			envName:  "GOOGLE_APPLICATION_CREDENTIALS",
			expected: true,
		},
		{
			name: "env exists",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/some/path"},
				},
			},
			envName:  "GOOGLE_APPLICATION_CREDENTIALS",
			expected: false,
		},
		{
			name: "other env exists",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{Name: "OTHER_ENV", Value: "value"},
				},
			},
			envName:  "GOOGLE_APPLICATION_CREDENTIALS",
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := needsEnvVar(test.container, test.envName)
			if actual != test.expected {
				t.Errorf("needsEnvVar(..., %q) = %v; want %v", test.envName, actual, test.expected)
			}
		})
	}
}

func createPodAdmissionReview(t *testing.T, pod corev1.Pod, ns string) []byte {
	podBytes, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("failed to marshal pod: %v", err)
	}
	ar := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid-123",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			Namespace: ns,
			Object: runtime.RawExtension{
				Raw: podBytes,
			},
		},
	}
	arBytes, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("failed to marshal AdmissionReview: %v", err)
	}
	return arBytes
}

func createSAAdmissionReview(t *testing.T, sa corev1.ServiceAccount, ns string) []byte {
	saBytes, err := json.Marshal(sa)
	if err != nil {
		t.Fatalf("failed to marshal service account: %v", err)
	}
	ar := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid-456",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ServiceAccount",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "serviceaccounts",
			},
			Namespace: ns,
			Object: runtime.RawExtension{
				Raw: saBytes,
			},
		},
	}
	arBytes, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("failed to marshal AdmissionReview: %v", err)
	}
	return arBytes
}

func TestMutateHandler(t *testing.T) {
	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mutate", nil)
		rec := httptest.NewRecorder()
		mutateHandler(rec, req)

		res := rec.Result()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(body), "empty body") {
			t.Errorf("expected 'empty body' response, got %q", string(body))
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewBufferString("invalid json"))
		rec := httptest.NewRecorder()
		mutateHandler(rec, req)

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", res.StatusCode)
		}
		var ar admissionv1.AdmissionReview
		if err := json.NewDecoder(res.Body).Decode(&ar); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if ar.Response == nil || ar.Response.Result == nil || ar.Response.Result.Message == "" {
			t.Errorf("expected error response, got empty/nil response: %+v", ar.Response)
		}
	})

	t.Run("kube-system namespace pod should not be mutated", func(t *testing.T) {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "kube-system",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "app",
						Env:  []corev1.EnvVar{},
					},
				},
			},
		}
		payload := createPodAdmissionReview(t, pod, "kube-system")
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		mutateHandler(rec, req)

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", res.StatusCode)
		}

		var ar admissionv1.AdmissionReview
		if err := json.NewDecoder(res.Body).Decode(&ar); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !ar.Response.Allowed {
			t.Error("expected allowed to be true")
		}
		if len(ar.Response.Patch) > 0 && string(ar.Response.Patch) != "null" {
			t.Errorf("expected no patch for kube-system pod, got: %s", string(ar.Response.Patch))
		}
	})

	t.Run("standard namespace pod requiring credentials should be patched", func(t *testing.T) {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "app",
						Env:  []corev1.EnvVar{},
					},
				},
			},
		}
		payload := createPodAdmissionReview(t, pod, "default")
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		mutateHandler(rec, req)

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", res.StatusCode)
		}

		var ar admissionv1.AdmissionReview
		if err := json.NewDecoder(res.Body).Decode(&ar); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !ar.Response.Allowed {
			t.Error("expected allowed to be true")
		}
		if len(ar.Response.Patch) == 0 {
			t.Fatal("expected patch, got none")
		}

		patchStr := string(ar.Response.Patch)
		if !strings.Contains(patchStr, "gcp-creds") {
			t.Errorf("patch should contain gcp-creds volume, got: %s", patchStr)
		}
		if !strings.Contains(patchStr, "GOOGLE_APPLICATION_CREDENTIALS") {
			t.Errorf("patch should contain GOOGLE_APPLICATION_CREDENTIALS env var, got: %s", patchStr)
		}
	})
}

func TestServiceAccountHandler(t *testing.T) {
	t.Run("standard namespace serviceaccount should add pull secrets patch", func(t *testing.T) {
		sa := corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "default-sa",
			},
		}
		payload := createSAAdmissionReview(t, sa, "default")
		req := httptest.NewRequest(http.MethodPost, "/mutate/sa", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		serviceaccountHandler(rec, req)

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", res.StatusCode)
		}

		var ar admissionv1.AdmissionReview
		if err := json.NewDecoder(res.Body).Decode(&ar); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !ar.Response.Allowed {
			t.Error("expected allowed to be true")
		}
		if len(ar.Response.Patch) == 0 {
			t.Fatal("expected patch, got none")
		}

		patchStr := string(ar.Response.Patch)
		if !strings.Contains(patchStr, "gcp-auth") {
			t.Errorf("patch should contain gcp-auth pull secret, got: %s", patchStr)
		}
	})
}
