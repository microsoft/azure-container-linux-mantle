// Copyright 2021 Kinvolk GmbH
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package kubeadm

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"text/template"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/pkg/capnslog"

	"github.com/flatcar/mantle/kola"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"github.com/flatcar/mantle/kola/tests/etcd"
	tutil "github.com/flatcar/mantle/kola/tests/util"
	"github.com/flatcar/mantle/platform"
	"github.com/flatcar/mantle/platform/conf"
	"github.com/flatcar/mantle/util"
)

// extraTest is a regular test except that the `runFunc` takes
// a kubernetes controller as parameter in order to run the test commands from the
// controller node.
type extraTest struct {
	// name is the name of the test.
	name string
	// runFunc is step to run in order to perform the actual test. Controller is the Kubernetes node
	// from where the commands are ran.
	runFunc func(m platform.Machine, p map[string]interface{}, c cluster.TestCluster)
}

const (
	aclFlannelVersion        = "v0.25.4"
	aclFlannelSourceImage    = "docker.io/flannel/flannel:v0.25.4"
	aclFlannelCNISourceImage = "docker.io/flannel/flannel-cni-plugin:v1.4.1-flannel1"
	aclFlannelImageIndex     = "sha256:4cb3c3d29a1340cbaf43e2d7ee2de2afa627defeb4b11e65040a80b05bfd94d4"
	aclFlannelCNIImageIndex  = "sha256:3cf5509497ab6b2cbd08dd756ea304c74c39e15e7e86c142272a4ed1ff7b20fe"
	aclFlannelImage          = "mcr.microsoft.com/oss/v2/flannel-io/flannel:v0.25.4-1@" + aclFlannelImageIndex
	aclFlannelCNIImage       = "mcr.microsoft.com/oss/v2/flannel-io/flannel-cni-plugin:v1.4.1-19@" + aclFlannelCNIImageIndex
)

var (
	// extraTests can be used to extend the common tests for a given supported CNI.
	extraTests = map[string][]extraTest{
		"cilium": []extraTest{
			extraTest{
				name: "IPSec encryption",
				runFunc: func(controller platform.Machine, params map[string]interface{}, c cluster.TestCluster) {
					_ = c.MustSSH(controller, "/opt/bin/cilium uninstall")
					version := params["CiliumVersion"].(string)
					cidr := params["PodSubnet"].(string)
					cmd := fmt.Sprintf("/opt/bin/cilium install --config enable-endpoint-routes=true --config cluster-pool-ipv4-cidr=%s --version=%s --encryption=ipsec --wait=false --restart-unmanaged-pods=false --rollback=false", cidr, version)
					_, _ = c.SSH(controller, cmd)
					patch := `{ grep -q svirt_lxc_file_t /etc/selinux/mcs/contexts/lxc_contexts && kubectl --namespace kube-system patch daemonset/cilium -p '{"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","securityContext":{"seLinuxOptions":{"level":"s0","type":"unconfined_t"}}}],"initContainers":[{"name":"mount-cgroup","securityContext":{"seLinuxOptions":{"level":"s0","type":"unconfined_t"}}},{"name":"apply-sysctl-overwrites","securityContext":{"seLinuxOptions":{"level":"s0","type":"unconfined_t"}}},{"name":"clean-cilium-state","securityContext":{"seLinuxOptions":{"level":"s0","type":"unconfined_t"}}}]}}}}'; } || true`
					_ = c.MustSSH(controller, patch)
					status := "/opt/bin/cilium status --wait --wait-duration 1m"
					_ = c.MustSSH(controller, status)
				},
			},
		},
	}

	// CNIs is the list of CNIs to deploy
	// in the cluster setup
	CNIs = []string{
		"calico",
		"flannel",
		"cilium",
	}
	// testConfig holds params for various kubernetes releases
	// and the nested params are used to render script templates
	testConfig = map[string]map[string]interface{}{
		"v1.34.1": map[string]interface{}{
			"HelmVersion":     "v3.17.3",
			"MinMajorVersion": 3374,
			// from https://github.com/flannel-io/flannel/releases
			"FlannelVersion": "v0.26.7",
			// from https://github.com/cilium/cilium/releases
			"CiliumVersion": "1.12.5",
			// from https://github.com/cilium/cilium-cli/releases
			"CiliumCLIVersion": "v0.12.12",
			"DownloadDir":      "/opt/bin",
			"PodSubnet":        "192.168.0.0/17",
			"cgroupv1":         false,
		},
		"v1.33.0": map[string]interface{}{
			"HelmVersion":     "v3.17.3",
			"MinMajorVersion": 3374,
			// from https://github.com/flannel-io/flannel/releases
			"FlannelVersion": "v0.26.7",
			// from https://github.com/cilium/cilium/releases
			"CiliumVersion": "1.12.5",
			// from https://github.com/cilium/cilium-cli/releases
			"CiliumCLIVersion": "v0.12.12",
			"DownloadDir":      "/opt/bin",
			"PodSubnet":        "192.168.0.0/17",
			"cgroupv1":         false,
		},
		"v1.32.4": map[string]interface{}{
			"HelmVersion":     "v3.17.0",
			"MinMajorVersion": 3374,
			// from https://github.com/flannel-io/flannel/releases
			"FlannelVersion": "v0.22.0",
			// from https://github.com/cilium/cilium/releases
			"CiliumVersion": "1.12.5",
			// from https://github.com/cilium/cilium-cli/releases
			"CiliumCLIVersion": "v0.12.12",
			"DownloadDir":      "/opt/bin",
			"PodSubnet":        "192.168.0.0/17",
			"cgroupv1":         false,
		},
	}
	plog       = capnslog.NewPackageLogger("github.com/flatcar/mantle", "kola/tests/kubeadm")
	etcdConfig = conf.ContainerLinuxConfig(`
etcd:
  version: 3.5.22
  advertise_client_urls: http://{PRIVATE_IPV4}:2379
  listen_client_urls: http://0.0.0.0:2379`)
)

func configureACLFlannelImages(params map[string]interface{}, distribution, arch string) error {
	if params["CNI"] != "flannel" || distribution != "acl" {
		return nil
	}

	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture %q for ACL Flannel images", arch)
	}

	params["FlannelVersion"] = aclFlannelVersion
	params["FlannelSourceImage"] = aclFlannelSourceImage
	params["FlannelCNISourceImage"] = aclFlannelCNISourceImage
	params["FlannelImage"] = aclFlannelImage
	params["FlannelCNIImage"] = aclFlannelCNIImage
	params["FlannelImageIndex"] = aclFlannelImageIndex
	params["FlannelCNIImageIndex"] = aclFlannelCNIImageIndex
	return nil
}

// etcdConfigAclWithCIDR returns the ACL etcd-node Container Linux Config with
// an iptables INPUT ACCEPT rule for the given source CIDR, which lets the
// master/worker nodes reach etcd:2379. CIDR is taken from
// --trusted-source-cidr (default 10.0.0.0/8 in platform.Options).
func etcdConfigAclWithCIDR(cidr string) *conf.UserData {
	return conf.ContainerLinuxConfig(fmt.Sprintf(`
etcd:
  version: 3.5.22
  advertise_client_urls: http://{PRIVATE_IPV4}:2379
  listen_client_urls: http://0.0.0.0:2379
systemd:
  units:
    - name: "etcd-firewall.service"
      enabled: true
      contents: |-
        [Unit]
        Description=Open firewall for etcd inter-node traffic
        Before=etcd-member.service
        After=iptables.service

        [Service]
        Type=oneshot
        RemainAfterExit=true
        ExecStart=/usr/sbin/iptables -A INPUT -s %s -j ACCEPT

        [Install]
        WantedBy=multi-user.target`, cidr))
}

func init() {
	testConfigCgroupV1 := map[string]map[string]interface{}{}
	testConfigCgroupV1["v1.32.4"] = map[string]interface{}{}
	for k, v := range testConfig["v1.32.4"] {
		testConfigCgroupV1["v1.32.4"][k] = v
	}
	testConfigCgroupV1["v1.32.4"]["cgroupv1"] = true

	registerTests := func(config map[string]map[string]interface{}) {
		for version, params := range config {
			for _, CNI := range CNIs {
				flags := []register.Flag{register.NeedsDocker}
				// ugly but required to remove the reference between params and the params
				// actually used by the test.
				testParams := make(map[string]interface{})
				for k, v := range params {
					testParams[k] = v
				}
				testParams["CNI"] = CNI
				testParams["Release"] = version

				cgroupSuffix := ""
				var majorMinVersion int64 = 0
				var majorEndVersion int64 = math.MaxInt64
				if testParams["cgroupv1"].(bool) {
					cgroupSuffix = ".cgroupv1"
					majorMinVersion = 3140
					majorEndVersion = 4179
				}

				if CNI == "flannel" {
					flags = append(flags, register.NoEnableSelinux)
				}

				if mmvi, ok := testParams["MinMajorVersion"]; ok {
					mmv := (int64)(mmvi.(int))
					// Careful, so we don't lower
					// the min version too much.
					if mmv > majorMinVersion {
						majorMinVersion = mmv
					}
				}

				register.Register(&register.Test{
					Name:    fmt.Sprintf("kubeadm.%s.%s%s.base", version, CNI, cgroupSuffix),
					Distros: []string{"acl", "cl"},
					// This should run on all clouds as a good end-to-end test
					// Network config problems in qemu-unpriv
					ExcludePlatforms: []string{"qemu-unpriv"},
					Run: func(c cluster.TestCluster) {
						kubeadmBaseTest(c, testParams)
					},
					MinVersion: semver.Version{Major: majorMinVersion},
					EndVersion: semver.Version{Major: majorEndVersion},
					Flags:      flags,
				})
			}
		}
	}
	registerTests(testConfig)
	registerTests(testConfigCgroupV1)
}

// kubeadmBaseTest asserts that the cluster is up and running
func kubeadmBaseTest(c cluster.TestCluster, params map[string]interface{}) {
	params["Platform"] = c.Platform()
	arch := strings.SplitN(kola.QEMUOptions.Board, "-", 2)[0]
	params["Arch"] = arch
	if err := configureACLFlannelImages(params, kola.Options.Distribution, arch); err != nil {
		c.Fatalf("unable to configure Flannel images: %v", err)
	}
	kubectl, err := setup(c, params)
	if err != nil {
		c.Fatalf("unable to setup cluster: %v", err)
	}

	c.Run("node readiness", func(c cluster.TestCluster) {
		// Wait up to 3 min (36*5 = 180s) for nginx. The test can be flaky on overcommitted platforms.
		if err := util.Retry(36, 5*time.Second, func() error {
			// notice the extra space before "Ready", it's to not catch
			// "NotReady" nodes
			out := c.MustSSH(kubectl, "kubectl get nodes | grep \" Ready\"| wc -l")
			readyNodesCnt := string(out)
			if readyNodesCnt != "2" {
				return fmt.Errorf("ready nodes should be equal to 2: %s", readyNodesCnt)
			}

			return nil
		}); err != nil {
			c.Fatalf("nodes are not ready: %v", err)
		}
	})

	if _, ok := params["FlannelImage"]; ok {
		c.Run("flannel images", func(c cluster.TestCluster) {
			if err := verifyACLFlannelImages(c, kubectl, params); err != nil {
				c.Fatalf("unable to verify Flannel images: %v", err)
			}
		})
	}

	c.Run("nginx deployment", func(c cluster.TestCluster) {
		// nginx manifest has been deployed through ignition
		if _, err := c.SSH(kubectl, "kubectl apply -f nginx.yaml"); err != nil {
			c.Fatalf("unable to deploy nginx: %v", err)
		}

		// Wait up to 3 min (36*5 = 180s) for nginx. The test can be flaky on overcommitted platforms.
		if err := util.Retry(36, 5*time.Second, func() error {
			out := c.MustSSH(kubectl, "kubectl get deployments -o json | jq '.items | .[] | .status.readyReplicas'")
			readyCnt := string(out)
			if readyCnt != "1" {
				return fmt.Errorf("ready replicas should be equal to 1: %s", readyCnt)
			}

			return nil
		}); err != nil {
			c.Fatalf("nginx is not deployed: %v", err)
		}
	})

	c.Run("NFS deployment", func(c cluster.TestCluster) {
		if _, err := c.SSH(kubectl, "/opt/bin/helm repo add nfs-ganesha-server-and-external-provisioner https://kubernetes-sigs.github.io/nfs-ganesha-server-and-external-provisioner/"); err != nil {
			c.Fatalf("unable to add helm NFS repo: %v", err)
		}

		if _, err := c.SSH(kubectl, "/opt/bin/helm install nfs-server-provisioner nfs-ganesha-server-and-external-provisioner/nfs-server-provisioner --set 'storageClass.mountOptions={nfsvers=4.2}'"); err != nil {
			c.Fatalf("unable to install NFS Helm Chart: %v", err)
		}

		// Manifests have been deployed through Ignition
		if _, err := c.SSH(kubectl, "kubectl apply -f nfs-pod.yaml -f nfs-pvc.yaml"); err != nil {
			c.Fatalf("unable to create NFS pod and pvc: %v", err)
		}

		// Wait up to 3 min (36*5 = 180s). The test can be flaky on overcommitted platforms.
		if err := util.Retry(36, 5*time.Second, func() error {
			out, err := c.SSH(kubectl, `kubectl get pod/test-pod-1 -o json | jq '.status.containerStatuses[] | select (.name == "test") | .ready'`)
			if err != nil {
				return fmt.Errorf("getting container status: %v", err)
			}

			ready := string(out)
			if ready != "true" {
				return fmt.Errorf("'test' pod should be ready, got: %s", ready)
			}

			return nil
		}); err != nil {
			c.Fatalf("nginx pod with NFS is not deployed: %v", err)
		}
	})

	// this should not fail, we always have the CNI present at this step.
	cni, ok := params["CNI"]
	if !ok {
		c.Fatalf("CNI is not available in the runtime params")
	}

	// based on the CNI, we fetch the list of extra tests to run.
	extras, ok := extraTests[cni.(string)]
	if ok {
		for _, extra := range extras {
			t := extra.runFunc
			c.Run(extra.name, func(c cluster.TestCluster) { t(kubectl, params, c) })
		}
	}
}

func verifyACLFlannelImages(c cluster.TestCluster, controller platform.Machine, params map[string]interface{}) error {
	requiredParams := []string{
		"FlannelImage",
		"FlannelCNIImage",
		"FlannelImageIndex",
		"FlannelCNIImageIndex",
	}
	values := make(map[string]string, len(requiredParams))
	for _, key := range requiredParams {
		value, ok := params[key].(string)
		if !ok || value == "" {
			return fmt.Errorf("missing required %s parameter", key)
		}
		values[key] = value
	}

	command := fmt.Sprintf(`set -euo pipefail
expected_flannel_image=%q
expected_cni_image=%q
expected_flannel_index=%q
expected_cni_index=%q

kubectl --namespace kube-flannel rollout status daemonset/kube-flannel-ds --timeout=3m
daemonset_json="$(kubectl --namespace kube-flannel get daemonset/kube-flannel-ds -o json)"

assert_equal() {
	local label="$1"
	local expected="$2"
	local actual="$3"
	if [[ "${actual}" != "${expected}" ]]; then
		echo "${label}: expected ${expected}, got ${actual}" >&2
		exit 1
	fi
}

actual_flannel_image="$(jq -r '.spec.template.spec.containers[] | select(.name == "kube-flannel") | .image' <<<"${daemonset_json}")"
actual_install_cni_image="$(jq -r '.spec.template.spec.initContainers[] | select(.name == "install-cni") | .image' <<<"${daemonset_json}")"
actual_cni_image="$(jq -r '.spec.template.spec.initContainers[] | select(.name == "install-cni-plugin") | .image' <<<"${daemonset_json}")"
assert_equal "kube-flannel image" "${expected_flannel_image}" "${actual_flannel_image}"
assert_equal "install-cni image" "${expected_flannel_image}" "${actual_install_cni_image}"
assert_equal "install-cni-plugin image" "${expected_cni_image}" "${actual_cni_image}"

if ! jq -e --arg flannel "${expected_flannel_image}" --arg cni "${expected_cni_image}" '
  [(.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?) | .image]
  | length == 3 and all(. == $flannel or . == $cni)
' <<<"${daemonset_json}" >/dev/null; then
	echo "Flannel DaemonSet contains an unexpected or missing image:" >&2
	jq -r '(.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?) | "\(.name): \(.image)"' <<<"${daemonset_json}" >&2 || true
	exit 1
fi

pods_json="$(kubectl --namespace kube-flannel get pods --selector app=flannel -o json)"
if ! pod_statuses="$(jq -er '
  def image_id($statuses; $container; $pod):
    [$statuses[]? | select(.name == $container) | .imageID]
    | if length != 1 or .[0] == null or .[0] == "" then
        error("\($pod): expected exactly one non-empty \($container) imageID")
      else .[0]
      end;

  [.items[] | select(.metadata.deletionTimestamp == null)] as $pods
  | if ($pods | length) == 0 then
      error("no non-terminating Flannel pods found")
    else
      $pods[]
      | .metadata.name as $pod
      | [
          $pod,
          image_id(.status.containerStatuses; "kube-flannel"; $pod),
          image_id(.status.initContainerStatuses; "install-cni"; $pod),
          image_id(.status.initContainerStatuses; "install-cni-plugin"; $pod)
        ]
      | @tsv
    end
' <<<"${pods_json}")"; then
	echo "Unable to read complete Flannel image status for every pod" >&2
	exit 1
fi

assert_image_id() {
	local label="$1"
	local expected_index="$2"
	local image_id="$3"
	if [[ "${image_id}" != *"@${expected_index}" ]]; then
		echo "${label}: expected index ${expected_index}, got ${image_id}" >&2
		exit 1
	fi
}


while IFS=$'	' read -r pod flannel_image_id install_cni_image_id cni_image_id; do
	assert_image_id "${pod}/kube-flannel" "${expected_flannel_index}" "${flannel_image_id}"
	assert_image_id "${pod}/install-cni" "${expected_flannel_index}" "${install_cni_image_id}"
	assert_image_id "${pod}/install-cni-plugin" "${expected_cni_index}" "${cni_image_id}"
done <<<"${pod_statuses}"`,
		values["FlannelImage"], values["FlannelCNIImage"],
		values["FlannelImageIndex"], values["FlannelCNIImageIndex"])

	if _, err := c.SSH(controller, command); err != nil {
		return fmt.Errorf("checking the Flannel DaemonSet: %w", err)
	}
	return nil
}

// render takes care of template rendering
// using `b` parameter, we can render in a base64 encoded format
func render(s string, p map[string]interface{}, b bool) (*bytes.Buffer, error) {
	tmpl, err := template.New("install").Parse(s)
	if err != nil {
		return nil, fmt.Errorf("unable to parse script: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return nil, fmt.Errorf("unable to execute template: %w", err)
	}

	if b {
		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
		buf.Reset()
		if _, err := buf.WriteString(b64); err != nil {
			return nil, fmt.Errorf("unable to write bas64 content to buffer: %w", err)
		}
	}

	return &buf, nil
}

// setup creates a cluster with kubeadm
// cluster is composed by etcd node, worker and master node
// it returns master node in order to have direct access on node
// with kubectl installed and setup
func setup(c cluster.TestCluster, params map[string]interface{}) (platform.Machine, error) {
	plog.Infof("creating etcd node")

	useConfig := etcdConfig
	if kola.Options.Distribution == "acl" {
		useConfig = etcdConfigAclWithCIDR(kola.Options.TrustedSourceCIDR)
	}
	etcdNode, err := c.NewMachine(useConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create etcd node: %w", err)
	}

	etcdIPCorrected, err := correctAzurePrivateIP(c, etcdNode)
	if err != nil {
		return nil, fmt.Errorf("unable to configure Azure etcd node private IP: %w", err)
	}
	if etcdIPCorrected {
		if _, stderr, err := etcdNode.SSH("sudo systemctl restart etcd-member.service"); err != nil {
			return nil, fmt.Errorf("unable to restart etcd after correcting its Azure private IP: %w: %s", err, stderr)
		}
	}

	v := string(c.MustSSH(etcdNode, `set -euo pipefail; grep -m 1 "^VERSION=" /usr/lib/os-release | cut -d = -f 2`))
	if v == "" {
		c.Fatalf("Assertion for version string failed")
	}

	version, err := semver.NewVersion(v)
	if err != nil {
		c.Fatalf("unable to create semver version from %s: %v", version, err)
	}

	// For Cilium CNI, we enforce SELinux only for version >= 3745 because the SELinux policies update (container_t/spc_t) is not yet
	// propagated through all the channels.
	// The etcd node will run with enforced SELinux anyway but we want to test SELinux on the worker / master nodes.
	cni, ok := params["CNI"]
	if !ok {
		c.Fatal("unable to get CNI value")
	}

	if cni == "cilium" && version.LessThan(semver.Version{Major: 3745}) {
		r := c.RuntimeConf()
		if r != nil {
			plog.Infof("Setting SELinux to permissive mode")
			r.NoEnableSelinux = true
		}
	}

	if err := etcd.GetClusterHealth(c, etcdNode, 1); err != nil {
		return nil, fmt.Errorf("unable to get etcd node health: %w", err)
	}

	params["Endpoints"] = []string{fmt.Sprintf("http://%s:2379", etcdNode.PrivateIP())}

	plog.Infof("creating master node")

	mScript, err := render(masterScript, params, true)
	if err != nil {
		return nil, fmt.Errorf("unable to render master script: %w", err)
	}

	params["MasterScript"] = mScript.String()

	openFirewall := false
	if kola.Options.Distribution == "acl" {
		openFirewall = true
	}
	params["openFirewall"] = openFirewall
	params["TrustedCIDR"] = kola.Options.TrustedSourceCIDR

	masterCfg, err := render(masterConfig, params, false)
	if err != nil {
		return nil, fmt.Errorf("unable to render container linux config for master: %w", err)
	}

	var master, worker platform.Machine
	p := c.Platform()
	isQemu := p == "qemu" || p == "qemu-unpriv"
	if isQemu {
		master, err = tutil.NewMachineWithLargeDisk(c, "5G", conf.Butane(masterCfg.String()))
		if err != nil {
			return nil, fmt.Errorf("unable to create master node with large disk: %w", err)
		}
	} else {
		master, err = c.NewMachine(conf.Butane(masterCfg.String()))
		if err != nil {
			return nil, fmt.Errorf("unable to create master node: %w", err)
		}
	}

	out, err := c.SSH(master, "sudo /home/core/install.sh")
	if err != nil {
		return nil, fmt.Errorf("unable to run master script: %w", err)
	}

	// "out" holds the worker config generated
	// by the master script install
	params["WorkerConfig"] = string(out)

	plog.Infof("creating worker node")
	wScript, err := render(workerScript, params, true)
	if err != nil {
		return nil, fmt.Errorf("unable to render worker script: %w", err)
	}

	params["WorkerScript"] = wScript.String()

	workerCfg, err := render(workerConfig, params, false)
	if err != nil {
		return nil, fmt.Errorf("unable to render container linux config for master: %w", err)
	}

	if isQemu {
		worker, err = tutil.NewMachineWithLargeDisk(c, "5G", conf.Butane(workerCfg.String()))
		if err != nil {
			return nil, fmt.Errorf("unable to create worker node with large disk: %w", err)
		}
	} else {
		worker, err = c.NewMachine(conf.Butane(workerCfg.String()))
		if err != nil {
			return nil, fmt.Errorf("unable to create worker node: %w", err)
		}
	}

	if _, err := correctAzurePrivateIP(c, worker); err != nil {
		return nil, fmt.Errorf("unable to configure Azure worker node private IP: %w", err)
	}

	out, err = c.SSH(worker, "sudo /home/core/install.sh")
	if err != nil {
		return nil, fmt.Errorf("unable to run worker script: %w", err)
	}

	return master, nil
}

func correctAzurePrivateIP(c cluster.TestCluster, machine platform.Machine) (bool, error) {
	if c.Platform() != "azure" {
		return false, nil
	}

	privateIP := machine.PrivateIP()
	// Verify Azure's NIC address matches the guest's primary route before overriding bad metadata.
	command := fmt.Sprintf(`set -euo pipefail
private_ip=%q
guest_ip="$(ip -4 -o route get 168.63.129.16 | sed -n 's/.* src \([^ ]*\).*/\1/p' | head -n 1)"
if [[ -z "${guest_ip}" ]]; then
	echo "Unable to determine the guest primary IPv4 address" >&2
	exit 1
fi
if [[ "${guest_ip}" != "${private_ip}" ]]; then
	echo "Azure NIC private IP ${private_ip} does not match guest primary IP ${guest_ip}" >&2
	exit 1
fi
sudo systemctl start --quiet coreos-metadata.service
current="$(sed -n 's/^COREOS_AZURE_IPV4_DYNAMIC=//p' /run/metadata/flatcar)"
if [[ "${current}" != "${private_ip}" ]]; then
	echo "Correcting Azure metadata private IP from ${current} to ${private_ip}"
	sudo cp -a /run/metadata/flatcar /run/metadata/flatcar.wireserver
	sudo sed -i "s/^COREOS_AZURE_IPV4_DYNAMIC=.*/COREOS_AZURE_IPV4_DYNAMIC=${private_ip}/" /run/metadata/flatcar
fi`, privateIP)
	stdout, stderr, err := machine.SSH(command)
	if err != nil {
		return false, fmt.Errorf("unable to correct Azure private IP: %w: %s", err, stderr)
	}
	message := strings.TrimSpace(string(stdout))
	if message != "" {
		plog.Infof("%s", message)
	}
	return message != "", nil
}
