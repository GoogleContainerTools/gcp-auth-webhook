//go:build integration
// +build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntegration(t *testing.T) {
	version := os.Getenv("GCP_AUTH_VERSION")
	if version == "" {
		version = "v0.1.4"
	}
	t.Logf("Running integration tests for version: %s", version)

	var profile string

	// 1. Ensure Minikube is running
	t.Run("Ensure Minikube Running", func(t *testing.T) {
		statusCmd := exec.Command("minikube", "status")
		if err := statusCmd.Run(); err != nil {
			t.Log("Minikube is not running. Starting Minikube...")
			startCmd := exec.Command("minikube", "start", "--interactive=false")
			startCmd.Stdout = os.Stdout
			startCmd.Stderr = os.Stderr
			if err := startCmd.Run(); err != nil {
				t.Fatalf("failed to start minikube: %v", err)
			}
		}

		profileBytes, err := exec.Command("minikube", "profile").Output()
		if err != nil {
			t.Fatalf("failed to get minikube profile: %v", err)
		}
		profile = strings.TrimSpace(strings.ReplaceAll(string(profileBytes), "*", ""))
		t.Logf("Using Minikube profile: '%s'", profile)
	})

	// 2. Parse 'minikube docker-env' variables and apply them to current process environment
	t.Run("Configure Docker Env", func(t *testing.T) {
		dockerEnvCmd := exec.Command("minikube", "-p", profile, "docker-env")
		dockerEnvOut, err := dockerEnvCmd.Output()
		if err != nil {
			t.Fatalf("failed to run minikube docker-env: %v", err)
		}

		scanner := bufio.NewScanner(bytes.NewReader(dockerEnvOut))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "export ") {
				kv := strings.TrimPrefix(line, "export ")
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					key := parts[0]
					val := strings.Trim(parts[1], `"`)
					os.Setenv(key, val)
				}
			}
		}
	})

	// 3. Setup temporary dummy credentials inside the workspace root (retains file till end of all subtests)
	tmpFile, err := os.CreateTemp("../..", "gcp-creds-*.json")
	if err != nil {
		t.Fatalf("failed to create temporary credentials file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	absCredsPath, err := filepath.Abs(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to get absolute path of credentials: %v", err)
	}

	dummySA := `{
  "type": "service_account",
  "project_id": "dummy-project-id",
  "private_key_id": "dummy-private-key-id",
  "private_key": "-----BEGIN PRIVATE KEY-----\nTHISISADUMMYPRIVATEKEYFORTESTINGONLYNOTAREALKEY\n-----END PRIVATE KEY-----\n",
  "client_email": "dummy-sa@dummy-project-id.iam.gserviceaccount.com",
  "client_id": "1234567890",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/annotations"
}`
	if _, err := tmpFile.WriteString(dummySA); err != nil {
		t.Fatalf("failed to write dummy credentials to file: %v", err)
	}
	tmpFile.Close()

	// 4. Build local image directly inside Minikube's docker daemon
	t.Run("Build Local Webhook Image", func(t *testing.T) {
		makeCmd := exec.Command("make", "local-image", "VERSION="+version)
		makeCmd.Dir = "../.."
		makeCmd.Stdout = os.Stdout
		makeCmd.Stderr = os.Stderr
		if err := makeCmd.Run(); err != nil {
			t.Fatalf("failed to build local image: %v", err)
		}
	})

	// 5. Enable the gcp-auth addon with the local image
	t.Run("Enable GCP Auth Addon", func(t *testing.T) {
		addonCmd := exec.Command("minikube", "addons", "enable", "gcp-auth",
			fmt.Sprintf("--images=GCPAuthWebhook=local/gcp-auth-webhook:%s", version),
			"--force", "--refresh")
		addonCmd.Env = append(os.Environ(), "GOOGLE_APPLICATION_CREDENTIALS="+absCredsPath)
		addonCmd.Stdout = os.Stdout
		addonCmd.Stderr = os.Stderr
		if err := addonCmd.Run(); err != nil {
			t.Fatalf("failed to enable gcp-auth addon: %v", err)
		}

		t.Log("Waiting for gcp-auth webhook deployment to be ready...")
		waitCmd := exec.Command("kubectl", "wait", "--namespace", "gcp-auth",
			"--for=condition=ready", "pod", "-l", "app=gcp-auth", "--timeout=3m")
		waitCmd.Stdout = os.Stdout
		waitCmd.Stderr = os.Stderr
		if err := waitCmd.Run(); err != nil {
			t.Fatalf("gcp-auth webhook pods did not become ready: %v", err)
		}
	})

	// 7. Setup test namespace
	t.Log("Creating test namespace 'gcp-auth-test'...")
	if err := exec.Command("kubectl", "create", "namespace", "gcp-auth-test").Run(); err != nil {
		t.Fatalf("failed to create test namespace: %v", err)
	}
	defer func() {
		t.Log("Cleaning up test namespace 'gcp-auth-test'...")
		exec.Command("kubectl", "delete", "namespace", "gcp-auth-test", "--ignore-not-found=true").Run()
	}()

	// Wait for default service account to be generated automatically
	time.Sleep(3 * time.Second)

	// 8. Verify Pod mutation
	t.Run("Verify Pod Mutation", func(t *testing.T) {
		runCmd := exec.Command("kubectl", "run", "test-pod", "--namespace", "gcp-auth-test",
			"--image=alpine", "--restart=Never", "--", "sleep", "3600")
		if err := runCmd.Run(); err != nil {
			t.Fatalf("failed to create test pod: %v", err)
		}
		time.Sleep(5 * time.Second)

		// Assert GOOGLE_APPLICATION_CREDENTIALS env var
		envBytes, err := exec.Command("kubectl", "get", "pod", "test-pod", "-n", "gcp-auth-test",
			"-o", "jsonpath={.spec.containers[0].env[?(@.name==\"GOOGLE_APPLICATION_CREDENTIALS\")].value}").Output()
		if err != nil {
			t.Fatalf("failed to get pod env: %v", err)
		}
		envVal := strings.TrimSpace(string(envBytes))
		if envVal != "/google-app-creds.json" {
			podYaml, _ := exec.Command("kubectl", "get", "pod", "test-pod", "-n", "gcp-auth-test", "-o", "yaml").Output()
			t.Logf("Pod YAML:\n%s", string(podYaml))
			t.Fatalf("Pod env GOOGLE_APPLICATION_CREDENTIALS is '%s', expected '/google-app-creds.json'", envVal)
		}

		// Assert gcp-creds volume mount
		mountBytes, err := exec.Command("kubectl", "get", "pod", "test-pod", "-n", "gcp-auth-test",
			"-o", "jsonpath={.spec.containers[0].volumeMounts[?(@.name==\"gcp-creds\")].mountPath}").Output()
		if err != nil {
			t.Fatalf("failed to get pod volume mounts: %v", err)
		}
		mountVal := strings.TrimSpace(string(mountBytes))
		if mountVal != "/google-app-creds.json" {
			podYaml, _ := exec.Command("kubectl", "get", "pod", "test-pod", "-n", "gcp-auth-test", "-o", "yaml").Output()
			t.Logf("Pod YAML:\n%s", string(podYaml))
			t.Fatalf("Pod volume mount path is '%s', expected '/google-app-creds.json'", mountVal)
		}
		t.Log("Pod mutation verified successfully!")
	})

	// 9. Verify ServiceAccount mutation
	t.Run("Verify ServiceAccount Mutation", func(t *testing.T) {
		if err := exec.Command("kubectl", "create", "serviceaccount", "test-sa", "--namespace", "gcp-auth-test").Run(); err != nil {
			t.Fatalf("failed to create test service account: %v", err)
		}
		time.Sleep(2 * time.Second)

		secretBytes, err := exec.Command("kubectl", "get", "sa", "test-sa", "-n", "gcp-auth-test",
			"-o", "jsonpath={.imagePullSecrets[?(@.name==\"gcp-auth\")].name}").Output()
		if err != nil {
			t.Fatalf("failed to get service account pull secrets: %v", err)
		}
		secretVal := strings.TrimSpace(string(secretBytes))
		if secretVal != "gcp-auth" {
			saYaml, _ := exec.Command("kubectl", "get", "sa", "test-sa", "-n", "gcp-auth-test", "-o", "yaml").Output()
			t.Logf("ServiceAccount YAML:\n%s", string(saYaml))
			t.Fatalf("ServiceAccount pull secret is '%s', expected 'gcp-auth'", secretVal)
		}

		t.Log("ServiceAccount mutation verified successfully!")
	})

	t.Log("ALL INTEGRATION TESTS PASSED SUCCESSFULLY!")
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	_ = ctx

	os.Exit(m.Run())
}
