package misc

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/pkg/capnslog"
	"github.com/flatcar/mantle/kola"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	testsutil "github.com/flatcar/mantle/kola/tests/util"
	"github.com/flatcar/mantle/platform"
	"github.com/flatcar/mantle/platform/conf"
	"github.com/flatcar/mantle/util"
)

const (
	CmdTimeout           = time.Second * 300
	NvidiaSysextVersion  = "550-open"                         // NVIDIA drivers sysext version used in the template
	KubernetesVersion    = "v1.32.2"                          // Kubernetes version used in the template
	NvidiaRuntimeVersion = "v1.16.2"                          // NVIDIA runtime version used in the template
	GpuOperatorVersion   = "v24.9.2"                          // GPU operator version used for Helm install
	CudaSampleImageTag   = "vectoradd-cuda11.7.1-ubuntu20.04" // CUDA sample image tag
	AclGpuRepo           = "mcr.microsoft.com/azurelinux/3.0/azure-container-linux" // default MCR repo for ACL GPU sysexts
	OrasVersion          = "1.3.0"                            // ORAS CLI version for pulling OCI artifacts
)

const nvidiaOperatorTemplate = `
variant: flatcar
version: 1.0.0

storage:
  files:
  - path: /opt/extensions/kubernetes-{{ .KubernetesVersion }}-{{ .ARCH_SUFFIX }}.raw
    contents:
      source: https://extensions.flatcar.org/extensions/kubernetes-{{ .KubernetesVersion }}-{{ .ARCH_SUFFIX }}.raw
  - path: /opt/extensions/nvidia-runtime-{{ .NvidiaRuntimeVersion }}-{{ .ARCH_SUFFIX }}.raw
    contents:
      source: https://extensions.flatcar.org/extensions/nvidia-runtime-{{ .NvidiaRuntimeVersion }}-{{ .ARCH_SUFFIX }}.raw
  links:
  - path: /etc/extensions/kubernetes.raw
    target: /opt/extensions/kubernetes-{{ .KubernetesVersion }}-{{ .ARCH_SUFFIX }}.raw
    hard: false
  - path: /etc/extensions/nvidia-runtime.raw
    target: /opt/extensions/nvidia-runtime-{{ .NvidiaRuntimeVersion }}-{{ .ARCH_SUFFIX }}.raw
    hard: false
`

const nvidiaSysextTemplate = `
variant: flatcar
version: 1.0.0

storage:
  files:
  - path: /etc/flatcar/enabled-sysext.conf
    contents:
      inline: |
        nvidia-drivers-{{ .NvidiaSysextVersion }}
`

const nvidiaSysextOperatorTemplate = `
variant: flatcar
version: 1.0.0

storage:
  files:
  - path: /opt/extensions/kubernetes-{{ .KubernetesVersion }}-{{ .ARCH_SUFFIX }}.raw
    contents:
      source: https://extensions.flatcar.org/extensions/kubernetes-{{ .KubernetesVersion }}-{{ .ARCH_SUFFIX }}.raw
  - path: /opt/extensions/nvidia-runtime-{{ .NvidiaRuntimeVersion }}-{{ .ARCH_SUFFIX }}.raw
    contents:
      source: https://extensions.flatcar.org/extensions/nvidia-runtime-{{ .NvidiaRuntimeVersion }}-{{ .ARCH_SUFFIX }}.raw
  - path: /etc/flatcar/enabled-sysext.conf
    contents:
      inline: |
        nvidia-drivers-{{ .NvidiaSysextVersion }}
  links:
  - path: /etc/extensions/kubernetes.raw
    target: /opt/extensions/kubernetes-{{ .KubernetesVersion }}-{{ .ARCH_SUFFIX }}.raw
    hard: false
  - path: /etc/extensions/nvidia-runtime.raw
    target: /opt/extensions/nvidia-runtime-{{ .NvidiaRuntimeVersion }}-{{ .ARCH_SUFFIX }}.raw
    hard: false
`

var plog = capnslog.NewPackageLogger("github.com/flatcar/mantle", "kola/tests/misc")

func init() {
	register.Register(&register.Test{
		Name:          "cl.misc.nvidia",
		Run:           verifyNvidiaInstallation,
		ClusterSize:   0,
		Distros:       []string{"acl", "cl"},
		Platforms:     []string{"azure", "aws"},
		Architectures: []string{"amd64", "arm64"},
		Flags:         []register.Flag{register.NoEnableSelinux},
		SkipFunc:      skipOnNonGpu,
	})

	register.Register(&register.Test{
		Name:          "cl.misc.nvidia-operator",
		Run:           verifyNvidiaGpuOperator,
		ClusterSize:   0,
		Distros:       []string{"acl", "cl"},
		Platforms:     []string{"azure", "aws"},
		Architectures: []string{"amd64", "arm64"},
		Flags:         []register.Flag{register.NoEnableSelinux, register.NoEmergencyShellCheck},
		SkipFunc:      skipOnNonGpu,
	})

	register.Register(&register.Test{
		Name:          "cl.misc.nvidia-sysext",
		Run:           verifyNvidiaSysextInstallation,
		ClusterSize:   0,
		Distros:       []string{"acl", "cl"},
		Platforms:     []string{"azure", "aws"},
		Architectures: []string{"amd64", "arm64"},
		Flags:         []register.Flag{register.NoEnableSelinux},
		SkipFunc:      skipOnNonGpu,
		MinVersion:    semver.Version{Major: 4334},
	})

	register.Register(&register.Test{
		Name:          "cl.misc.nvidia-sysext-operator",
		Run:           verifyNvidiaSysextGpuOperator,
		ClusterSize:   0,
		Distros:       []string{"acl", "cl"},
		Platforms:     []string{"azure", "aws"},
		Architectures: []string{"amd64", "arm64"},
		Flags:         []register.Flag{register.NoEnableSelinux, register.NoEmergencyShellCheck},
		SkipFunc:      skipOnNonGpu,
		MinVersion:    semver.Version{Major: 4334},
	})

	register.Register(&register.Test{
		Name:          "acl.gpu.sysext",
		Run:           verifyAclGpuSysextInstallation,
		ClusterSize:   0,
		Distros:       []string{"acl"},
		Platforms:     []string{"azure"},
		Architectures: []string{"amd64"},
		// NoEnableSelinux: the NVIDIA driver and container-toolkit sysexts
		// currently fail under enforcing SELinux (same situation as the
		// cl.misc.nvidia-sysext-operator test above, which sets the same
		// flag). This test verifies the sysext install / activation path,
		// not the SELinux interaction; SELinux coverage for GPU is tracked
		// separately.
		Flags:    []register.Flag{register.NoEnableSelinux},
		SkipFunc: skipOnNonGpu,
	})
}

func skipOnNonGpu(_ semver.Version, _, arch, platform string) bool {
	// All Azure GPU SKUs start with NC, ND, or NV (NVIDIA-accelerated families).
	// AB-prefixed sizes are AMD GPUs and intentionally not covered here.
	if platform == "azure" {
		size := kola.AzureOptions.Size
		if strings.HasPrefix(size, "Standard_NC") ||
			strings.HasPrefix(size, "Standard_ND") ||
			strings.HasPrefix(size, "Standard_NV") {
			return false
		}
	}
	if platform == "aws" && (strings.HasPrefix(kola.AWSOptions.InstanceType, "p") || strings.HasPrefix(kola.AWSOptions.InstanceType, "g")) {
		return false
	}
	return true
}

func runtimeSkipOnNonGpu(c cluster.TestCluster) {
	if skipOnNonGpu(semver.Version{}, "", kola.QEMUOptions.Board, string(c.Platform())) {
		c.Skip("wrong instance size")
	}
}

func waitForNvidiaDriver(c *cluster.TestCluster, m *platform.Machine) error {
	nvidiaStatusRetry := func() error {
		out, err := c.SSH(*m, "systemctl status nvidia.service")
		if !bytes.Contains(out, []byte("active (exited)")) {
			return fmt.Errorf("nvidia.service: %q: %v", out, err)
		}
		return nil
	}

	if err := util.Retry(40, 15*time.Second, nvidiaStatusRetry); err != nil {
		return err
	}
	return nil
}

func verifyNvidiaSysextInstallationImpl(c cluster.TestCluster, sysextMode bool) {
	runtimeSkipOnNonGpu(c)
	var userData *conf.UserData
	if sysextMode {
		params := map[string]string{
			"NvidiaSysextVersion": NvidiaSysextVersion,
		}
		// For amd64 the suffix is x86-64, for arm64 it's arm64
		if kola.QEMUOptions.Board == "arm64-usr" {
			params["ARCH_SUFFIX"] = "arm64"
		} else {
			params["ARCH_SUFFIX"] = "x86-64"
		}

		butane, err := testsutil.ExecTemplate(nvidiaSysextTemplate, params)
		if err != nil {
			c.Fatalf("ExecTemplate: %s", err)
		}
		userData = conf.Butane(butane)
	}

	m, err := c.NewMachine(userData)
	if err != nil {
		c.Fatal(err)
	}
	if err := waitForNvidiaDriver(&c, &m); err != nil {
		c.Fatal(err)
	}
	nvidiaSmiPath := "/opt/bin/nvidia-smi"
	if sysextMode {
		nvidiaSmiPath = "/usr/bin/nvidia-smi"
	}
	out := c.MustSSH(m, nvidiaSmiPath)
	c.Logf("nvidia-smi: %s", out)
}

func verifyNvidiaSysextGpuOperatorImpl(c cluster.TestCluster, sysextMode bool) {
	runtimeSkipOnNonGpu(c)
	params := map[string]string{
		"NvidiaSysextVersion":  NvidiaSysextVersion,
		"KubernetesVersion":    KubernetesVersion,
		"NvidiaRuntimeVersion": NvidiaRuntimeVersion,
	}
	// For amd64 the suffix is x86-64, for arm64 it's arm64
	if kola.QEMUOptions.Board == "arm64-usr" {
		params["ARCH_SUFFIX"] = "arm64"
	} else {
		params["ARCH_SUFFIX"] = "x86-64"
	}

	template := nvidiaOperatorTemplate
	if sysextMode {
		template = nvidiaSysextOperatorTemplate
	}
	butane, err := testsutil.ExecTemplate(template, params)
	if err != nil {
		c.Fatalf("ExecTemplate: %s", err)
	}

	m, err := testsutil.NewMachineWithLargeDisk(c, "32G", conf.Butane(butane))
	if err != nil {
		c.Fatalf("Cluster.NewMachine: %s", err)
	}

	if err = waitForNvidiaDriver(&c, &m); err != nil {
		c.Fatal(err)
	}
	_ = c.MustSSH(m, "sudo systemctl cat nvidia.service")
	_ = c.MustSSH(m, "sudo systemd-sysext status")
	c.AssertCmdOutputContains(m, "sudo systemd-sysext status", "nvidia-runtime")
	if sysextMode {
		c.AssertCmdOutputContains(m, "sudo systemd-sysext status", "flatcar-nvidia-drivers")
	} else {
		c.AssertCmdOutputContains(m, "sudo systemd-sysext status", "nvidia-driver")
	}
	_ = c.MustSSH(m, `curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 \
	&& chmod 700 get_helm.sh \
	&& HELM_INSTALL_DIR=/opt/bin PATH=$PATH:/opt/bin ./get_helm.sh`)
	_ = c.MustSSH(m, "sudo kubeadm init --pod-network-cidr=10.244.0.0/16")
	_ = c.MustSSH(m, `mkdir -p $HOME/.kube
	sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
	sudo chown $(id -u):$(id -g) $HOME/.kube/config`)
	_ = c.MustSSH(m, "kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml")
	_ = c.MustSSH(m, "kubectl taint nodes --all node-role.kubernetes.io/control-plane-")
	_ = c.MustSSH(m, "kubectl describe nodes $HOSTNAME")
	err = util.Retry(5, 10*time.Second, func() error {
		out, err := c.SSH(m, "kubectl get nodes")
		if err != nil {
			return err
		}
		if strings.Contains(string(out), "NotReady") {
			return fmt.Errorf("nodes not ready: %s", string(out))
		}
		return nil
	})
	if err != nil {
		c.Fatalf("%v", err)
	}
	_ = c.MustSSH(m, "/opt/bin/helm repo add nvidia https://helm.ngc.nvidia.com/nvidia && /opt/bin/helm repo update")
	_ = c.MustSSH(m, fmt.Sprintf(`/opt/bin/helm install --wait --generate-name \
	-n gpu-operator --create-namespace \
	--version %s \
	nvidia/gpu-operator \
	--set driver.enabled=false \
	--set toolkit.enabled=false \
	`, GpuOperatorVersion))
	_ = c.MustSSH(m, "/opt/bin/helm ls")
	err = util.Retry(10, 10*time.Second, func() error {
		out, err := c.SSH(m, "kubectl get pods --all-namespaces -o json | jq '.items[] | select(.status.phase != \"Running\" and .status.phase != \"Succeeded\") | .metadata.name'")
		if err != nil {
			return err
		}
		lines := strings.Split(string(out), "\n")
		if len(lines) > 0 && lines[0] != "" {
			return fmt.Errorf("pods not ready: %d: %v", len(lines), lines)
		}
		return nil
	})
	_ = c.MustSSH(m, "kubectl get pods --all-namespaces")
	if err != nil {
		c.Fatalf("%v", err)
	}
	_ = c.MustSSH(m, fmt.Sprintf(`kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: cuda-vectoradd
spec:
  restartPolicy: OnFailure
  containers:
  - name: cuda-vectoradd
    image: "nvcr.io/nvidia/k8s/cuda-sample:%s"
    resources:
      limits:
        nvidia.com/gpu: 1
EOF`, CudaSampleImageTag))
	// wait until pod/cuda-vectoradd is done
	err = util.Retry(3, 10*time.Second, func() error {
		out, err := c.SSH(m, "kubectl get pod cuda-vectoradd -o jsonpath='{.status.phase}'")
		if err != nil {
			return err
		}
		if !strings.Contains(string(out), "Succeeded") {
			return fmt.Errorf("pod not ready: %s", string(out))
		}
		return nil
	})
	out := c.MustSSH(m, "kubectl get pods")
	c.Logf("get pods: %s", out)
	out = c.MustSSH(m, "kubectl logs pod/cuda-vectoradd")
	c.Logf("cuda-vectoradd logs: %s", out)
	if err != nil {
		c.Fatalf("%v", err)
	}
}

func verifyNvidiaInstallation(c cluster.TestCluster) {
	verifyNvidiaSysextInstallationImpl(c, false)
}

func verifyNvidiaSysextInstallation(c cluster.TestCluster) {
	verifyNvidiaSysextInstallationImpl(c, true)
}

func verifyNvidiaGpuOperator(c cluster.TestCluster) {
	verifyNvidiaSysextGpuOperatorImpl(c, false)
}

func verifyNvidiaSysextGpuOperator(c cluster.TestCluster) {
	verifyNvidiaSysextGpuOperatorImpl(c, true)
}

// verifyAclGpuSysextInstallation tests the ACL GPU sysext installation flow:
// install ORAS, pull GPU sysexts from a registry, deploy to /etc/extensions/,
// refresh systemd-sysext, and verify nvidia-smi, nvidia-ctk, and
// nvidia-fabricmanager.
//
// The registry is configurable via the ACL_GPU_REPO environment variable
// (defaults to MCR). This allows the pipeline to push build sysexts to an
// ACR and point the test at it.
func verifyAclGpuSysextInstallation(c cluster.TestCluster) {
	runtimeSkipOnNonGpu(c)

	gpuRepo := AclGpuRepo
	if envRepo := os.Getenv("ACL_GPU_REPO"); envRepo != "" {
		// Validate the repo URL to prevent shell injection — only allow
		// characters valid in registry/repo paths.
		if !regexp.MustCompile(`^[a-zA-Z0-9._\-/:]+$`).MatchString(envRepo) {
			c.Fatalf("ACL_GPU_REPO contains invalid characters: %q", envRepo)
		}
		gpuRepo = envRepo
	}
	c.Logf("Using GPU sysext repo: %s", gpuRepo)

	m, err := c.NewMachine(nil)
	if err != nil {
		c.Fatal(err)
	}

	// Install ORAS CLI
	_ = c.MustSSH(m, fmt.Sprintf(`
		sudo mkdir -p /opt/oras-install/ && \
		sudo curl -fsSL -o /opt/oras-install/oras_%s_linux_amd64.tar.gz \
			"https://github.com/oras-project/oras/releases/download/v%s/oras_%s_linux_amd64.tar.gz" && \
		sudo tar -zxf /opt/oras-install/oras_%s_linux_amd64.tar.gz -C /opt/oras-install/ && \
		sudo rm -f /opt/oras-install/oras_%s_linux_amd64.tar.gz
	`, OrasVersion, OrasVersion, OrasVersion, OrasVersion, OrasVersion))

	// Resolve and validate the driver flavor before any side-effecting work.
	// Keep the allow-list aligned with azure-container-linux/acl/sysexts.yaml
	// (nvidia-driver-* entries) and run-gpu-sysext-test.sh.
	driverFlavor := os.Getenv("ACL_GPU_DRIVER")
	if driverFlavor == "" {
		driverFlavor = "cuda-open"
	}
	if !regexp.MustCompile(`^(cuda-open|cuda|vgpu)$`).MatchString(driverFlavor) {
		c.Fatalf("ACL_GPU_DRIVER must be one of cuda-open, cuda, vgpu (got: %q)", driverFlavor)
	}
	driverSysext := "nvidia-driver-" + driverFlavor
	c.Logf("Using GPU driver sysext: %s", driverSysext)

	// If an ACR access token is provided, log in to the private registry
	// so ORAS can pull sysexts. The token is generated by the pipeline's
	// AzureCLI task and passed through as an environment variable.
	if acrToken := os.Getenv("ACR_ACCESS_TOKEN"); acrToken != "" {
		acrHost := strings.SplitN(gpuRepo, "/", 2)[0]
		c.Logf("Logging in to private registry %s", acrHost)
		_ = c.MustSSH(m, fmt.Sprintf(
			`export PATH="/opt/oras-install:$PATH" && oras login "%s" --username "00000000-0000-0000-0000-000000000000" --password "%s"`,
			acrHost, acrToken))
	}

	// Pull all three GPU sysext images.
	// Use ACL_SYSEXT_TAG (set by the pipeline) for build-scoped tags,
	// falling back to the VM's VERSION_ID for local/MCR testing.
	sysextTag := os.Getenv("ACL_SYSEXT_TAG")
	tagCmd := ""
	if sysextTag != "" {
		c.Logf("Using pipeline sysext tag: %s", sysextTag)
		tagCmd = fmt.Sprintf(`SYSEXT_TAG="%s"`, sysextTag)
	} else {
		c.Logf("No ACL_SYSEXT_TAG set, using VM VERSION_ID")
		tagCmd = `SYSEXT_TAG=$(. /etc/os-release && echo "${VERSION_ID}")`
	}

	_ = c.MustSSH(m, fmt.Sprintf(`
		export PATH="/opt/oras-install:$PATH" && \
		%s && \
		mkdir -p /tmp/sysext && \
		oras pull -o /tmp/sysext "%s/%s:${SYSEXT_TAG}" && \
		oras pull -o /tmp/sysext "%s/nvidia-container-toolkit:${SYSEXT_TAG}" && \
		oras pull -o /tmp/sysext "%s/nvidia-fabric-manager:${SYSEXT_TAG}"
	`, tagCmd, gpuRepo, driverSysext, gpuRepo, gpuRepo))

	// Deploy sysexts and refresh
	_ = c.MustSSH(m, `
		sudo find /tmp/sysext -name '*.raw' -exec mv {} /etc/extensions/ \; && \
		rm -rf /tmp/sysext && \
		sudo systemd-sysext refresh
	`)

	// Wait for nvidia-smi to become available after sysext refresh
	err = util.Retry(20, 15*time.Second, func() error {
		out, err := c.SSH(m, "sudo nvidia-smi")
		if err != nil {
			return fmt.Errorf("nvidia-smi: %q: %v", out, err)
		}
		return nil
	})
	if err != nil {
		c.Fatalf("nvidia-smi failed: %v", err)
	}
	out := c.MustSSH(m, "sudo nvidia-smi")
	c.Logf("nvidia-smi: %s", out)

	// Verify nvidia-modprobe can load the kernel module and create device
	// nodes. This is the step where SELinux issues caused GPU provisioning
	// failures in AKS (AgentBaker configGPUDrivers flow).
	out = c.MustSSH(m, "sudo nvidia-modprobe -u -c0")
	c.Logf("nvidia-modprobe: %s", out)

	// Verify nvidia-ctk is available
	out = c.MustSSH(m, "nvidia-ctk --version")
	c.Logf("nvidia-ctk: %s", out)

	// Verify systemd-sysext status shows GPU extensions
	_ = c.MustSSH(m, "sudo systemd-sysext status")

	// Start and verify nvidia-fabricmanager only on multi-GPU VMs.
	// Single-GPU VMs (NC-series) don't have NVLink/NVSwitch fabric.
	gpuCount, gpuCountErr := c.SSH(m, "sudo nvidia-smi --query-gpu=name --format=csv,noheader | wc -l")
	gpuCountStr := strings.TrimSpace(string(gpuCount))
	if gpuCountErr != nil {
		c.Logf("WARNING: failed to query GPU count: %v — assuming single GPU", gpuCountErr)
		gpuCountStr = "1"
	}
	_ = c.MustSSH(m, "sudo systemctl enable nvidia-fabricmanager")

	if gpuCountStr != "" && gpuCountStr != "1" {
		c.Logf("Multi-GPU detected (%s GPUs), starting fabric manager", gpuCountStr)
		_ = c.MustSSH(m, "sudo systemctl start nvidia-fabricmanager")
		err = util.Retry(5, 5*time.Second, func() error {
			out, err := c.SSH(m, "systemctl is-active nvidia-fabricmanager")
			if err != nil || strings.TrimSpace(string(out)) != "active" {
				return fmt.Errorf("nvidia-fabricmanager not active: %q: %v", out, err)
			}
			return nil
		})
		if err != nil {
			c.Fatalf("nvidia-fabricmanager failed to start: %v", err)
		}
		c.Logf("nvidia-fabricmanager: active")
	} else {
		c.Logf("Single GPU detected — skipping fabric manager start")
	}
}
