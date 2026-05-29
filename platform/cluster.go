// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package platform

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/pborman/uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/flatcar/mantle/platform/conf"
	"github.com/flatcar/mantle/util"
)

type BaseCluster struct {
	machlock   sync.Mutex
	machmap    map[string]Machine
	consolemap map[string]string

	bf    *BaseFlight
	name  string
	rconf *RuntimeConfig
}

func NewBaseCluster(bf *BaseFlight, rconf *RuntimeConfig) (*BaseCluster, error) {
	bc := &BaseCluster{
		bf:         bf,
		machmap:    make(map[string]Machine),
		consolemap: make(map[string]string),
		name:       fmt.Sprintf("%s-%s", bf.baseopts.BaseName, uuid.New()),
		rconf:      rconf,
	}

	return bc, nil
}

func (bc *BaseCluster) SSHClient(ip string) (*ssh.Client, error) {
	if bc.rconf.DefaultUser != "" {
		return bc.UserSSHClient(ip, bc.rconf.DefaultUser)
	}

	sshClient, err := bc.bf.agent.NewClient(ip)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

func (bc *BaseCluster) UserSSHClient(ip, user string) (*ssh.Client, error) {
	sshClient, err := bc.bf.agent.NewUserClient(ip, user)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

func (bc *BaseCluster) PasswordSSHClient(ip string, user string, password string) (*ssh.Client, error) {
	sshClient, err := bc.bf.agent.NewPasswordClient(ip, user, password)
	if err != nil {
		return nil, err
	}

	return sshClient, nil
}

// SSH executes the given command, cmd, on the given Machine, m. It returns the
// stdout and stderr of the command and an error.
// Leading and trailing whitespace is trimmed from each.
func (bc *BaseCluster) SSH(m Machine, cmd string) ([]byte, []byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	client, err := bc.SSHClient(m.IP())
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer session.Close()

	session.Stdout = &stdout
	session.Stderr = &stderr
	err = session.Run(cmd)
	outBytes := bytes.TrimSpace(stdout.Bytes())
	errBytes := bytes.TrimSpace(stderr.Bytes())
	return outBytes, errBytes, err
}

func (bc *BaseCluster) Machines() []Machine {
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	machs := make([]Machine, 0, len(bc.machmap))
	for _, m := range bc.machmap {
		machs = append(machs, m)
	}
	return machs
}

func (bc *BaseCluster) AddMach(m Machine) {
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	bc.machmap[m.ID()] = m
}

func (bc *BaseCluster) DelMach(m Machine) {
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	delete(bc.machmap, m.ID())
	bc.consolemap[m.ID()] = m.ConsoleOutput()
}

func (bc *BaseCluster) Keys() ([]*agent.Key, error) {
	return bc.bf.Keys()
}

func (bc *BaseCluster) RenderUserData(userdata *conf.UserData, ignitionVars map[string]string) (*conf.Conf, error) {
	if userdata == nil || userdata.IsEmpty() {
		switch bc.IgnitionVersion() {
		case "v2":
			userdata = conf.Ignition(`{"ignition": {"version": "2.0.0"}}`)
		case "v3":
			userdata = conf.Ignition(`{"ignition": {"version": "3.0.0"}}`)
		default:
			return nil, fmt.Errorf("unknown ignition version")
		}
	}

	u := bc.rconf.DefaultUser
	if u == "" {
		u = "core"
	}

	userdata.User = u

	// hacky solution for unified ignition metadata variables
	if userdata.IsIgnitionCompatible() {
		for k, v := range ignitionVars {
			userdata = userdata.Subst(k, v)
		}
	}

	if bc.bf.AdditionalSshKeys != nil && *bc.bf.AdditionalSshKeys != nil && !bc.rconf.NoSSHKeyInUserData {
		userdata = conf.AddSSHKeys(userdata, bc.bf.AdditionalSshKeys)
	}

	conf, err := userdata.Render(bc.bf.ctPlatform)
	if err != nil {
		return nil, err
	}

	// Validate username to prevent injection into systemd unit contents.
	// The username is interpolated into single-quoted sh -c commands in generated
	// systemd units, so restrict to POSIX-portable characters only.
	for _, ch := range u {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
			return nil, fmt.Errorf("invalid username %q: must contain only [a-zA-Z0-9_-]", u)
		}
	}

	// By default, the user is added to a sudo-capable group (for initial operations like enabling SELinux).
	if u != "core" {
		if err := conf.AddUserToGroups(u, []string{"sudo"}); err != nil {
			return nil, fmt.Errorf("adding user to group: %w", err)
		}
	}

	// ACL Phase 2: core is fully inert (/sbin/nologin, no wheel/docker/sudoers).
	// Without this block, all tests that SSH as core fail with
	// "no /etc/os-release" (really /sbin/nologin rejecting the session).
	// Fixes: cl.ignition.*, bpf.local-gadget, acl.flannel.*, cl.cloudinit.script, etc.
	//
	// Mechanisms by config type:
	//   1. AddSystemdUnit  — ignition (v2/v3): oneshot writes sudoers + usermod.
	//   2. AddFile          — script/multipart-mime: sudoers drop-in
	//      (silently dropped on ign-v2, but (1) covers that).
	//   3. AppendScriptCommands — script only: inline usermod before sshd.
	//      Fixes cl.cloudinit.script.
	//
	// cloud-config and multipart-mime have no mechanism to run usermod before
	// sshd, so cl.cloudinit.basic and cl.cloudinit.multipart-mime are excluded
	// from ACL in their test registrations (cloudinit.go).
	if bc.Distribution() == "acl" && u == "core" {
		// (1) Systemd unit for ignition configs:
		conf.AddSystemdUnit("kola-core-setup.service", `[Unit]
Description=Create core user for kola tests
After=systemd-sysusers.service systemd-sysext.service

[Service]
Type=oneshot
ExecStart=/bin/sh -c 'id core &>/dev/null || useradd -m -s /bin/bash -G wheel core'
ExecStart=/bin/sh -c 'id core &>/dev/null && usermod -s /bin/bash -a -G wheel core'
ExecStart=-/usr/sbin/usermod -a -G docker core
ExecStart=/bin/sh -c 'printf "core ALL=(ALL) NOPASSWD: ALL\n" > /etc/sudoers.d/kola-core-nopasswd && chmod 0440 /etc/sudoers.d/kola-core-nopasswd'
RemainAfterExit=true

[Install]
WantedBy=multi-user.target`, true)

		// (2) Sudoers file for script/multipart-mime (silently dropped on ign-v2):
		conf.AddFile("/etc/sudoers.d/kola-core-nopasswd", "root",
			"core ALL=(ALL) NOPASSWD: ALL\n", 0440)

		// (3) Inline commands for script configs (no-op on other types):
		conf.AppendScriptCommands("\n# Create core user for kola tests\n" +
			"id core &>/dev/null || useradd -m -s /bin/bash -G wheel core\n" +
			"id core &>/dev/null && usermod -s /bin/bash -a -G wheel core\n" +
			"usermod -a -G docker core 2>/dev/null || true\n")
	}

	// On ACL the docker group comes from the docker sysext (loaded after
	// systemd-sysusers), so no user can be added to it at build time.
	// Add the kola user to the docker group at boot. The "-" prefix on
	// ExecStart tolerates failure when docker sysext isn't loaded (group
	// doesn't exist). Some tests (e.g. sysext.custom-docker.sysext) build
	// their own docker sysext at runtime without setting IncludeDocker.
	// For core, kola-core-setup.service above already handles docker group.
	if bc.Distribution() == "acl" && u != "core" {
		conf.AddSystemdUnit("kola-docker-group.service", fmt.Sprintf(`[Unit]
Description=Add %s to docker group for kola tests
Before=docker.service containerd.service
After=systemd-sysusers.service systemd-sysext.service

[Service]
Type=oneshot
ExecStart=-/usr/sbin/usermod -a -G docker %s
RemainAfterExit=true

[Install]
WantedBy=multi-user.target`, u, u), true)
	}

	for _, dropin := range bc.bf.baseopts.SystemdDropins {
		conf.AddSystemdUnitDropin(dropin.Unit, dropin.Name, dropin.Contents)
	}

	if !bc.rconf.NoSSHKeyInUserData {
		keys, err := bc.bf.Keys()
		if err != nil {
			return nil, err
		}

		conf.CopyKeys(keys)
	}

	// disable the public update server by default
	if !bc.rconf.NoDisableUpdates {
		conf.AddFile("/etc/flatcar/update.conf", "root", `SERVER=disabled
`, 0644)
	}

	// disable Zincati & Pinger by default
	if bc.Distribution() == "fcos" {
		conf.AddFile("/etc/fedora-coreos-pinger/config.d/90-disable-reporting.toml", "root", `[reporting]
enabled = false`, 0644)
		conf.AddFile("/etc/zincati/config.d/90-disable-auto-updates.toml", "root", `[updates]
enabled = false`, 0644)
	}

	if bc.bf.baseopts.OSContainer != "" {
		if bc.Distribution() != "rhcos" {
			return nil, fmt.Errorf("oscontainer is only supported on the rhcos distribution")
		}
		conf.AddSystemdUnitDropin("pivot.service", "00-before-sshd.conf", `[Unit]
Before=sshd.service`)
		conf.AddSystemdUnit("pivot.service", "", true)
		conf.AddSystemdUnit("pivot-write-reboot-needed.service", `[Unit]
Description=Touch /run/pivot/reboot-needed
ConditionFirstBoot=true

[Service]
Type=oneshot
ExecStart=/usr/bin/mkdir -p /run/pivot
ExecStart=/usr/bin/touch /run/pivot/reboot-needed

[Install]
WantedBy=multi-user.target
`, true)
		conf.AddFile("/etc/pivot/image-pullspec", "root", bc.bf.baseopts.OSContainer, 0644)
	}

	if bc.Distribution() == "acl" && bc.rconf.IncludeDocker {
		conf.AddSystemdUnit("sysext-docker-link.service", `[Unit]
Description=Create symlink for docker sysext
DefaultDependencies=no
Before=systemd-sysext.service
After=local-fs.target

[Service]
Type=oneshot
RemainAfterExit=true
ExecStart=/usr/bin/ln -sf /oem/sysext/docker.raw /etc/extensions/docker.raw

[Install]
WantedBy=sysinit.target
`, true)
		conf.AddSystemdUnitDropin("systemd-sysext.service", "10-wait-for-docker-link.conf",
			`[Unit]
Wants=sysext-docker-link.service
After=sysext-docker-link.service
`)
	}

	if conf.IsIgnition() {
		if !conf.ValidConfig() {
			return nil, fmt.Errorf("invalid ignition config")
		}
	}

	return conf, nil
}

// Destroy destroys each machine in the cluster.
func (bc *BaseCluster) Destroy() {
	for _, m := range bc.Machines() {
		m.Destroy()
	}
}

// XXX(mischief): i don't really think this belongs here, but it completes the
// interface we've established.
func (bc *BaseCluster) GetDiscoveryURL(size int) (string, error) {
	var result string
	err := util.Retry(3, 5*time.Second, func() error {
		resp, err := http.Get(fmt.Sprintf("https://discovery.etcd.io/new?size=%d", size))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("Discovery service returned %q", resp.Status)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(body)
		return nil
	})
	return result, err
}

func (bc *BaseCluster) Distribution() string {
	return bc.bf.baseopts.Distribution
}

func (bc *BaseCluster) IgnitionVersion() string {
	return bc.bf.baseopts.IgnitionVersion
}

func (bc *BaseCluster) Platform() Name {
	return bc.bf.Platform()
}

func (bc *BaseCluster) Name() string {
	return bc.name
}

func (bc *BaseCluster) RuntimeConf() *RuntimeConfig {
	return bc.rconf
}

func (bc *BaseCluster) ConsoleOutput() map[string]string {
	ret := map[string]string{}
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	for k, v := range bc.consolemap {
		ret[k] = v
	}
	return ret
}

func (bc *BaseCluster) JournalOutput() map[string]string {
	ret := map[string]string{}
	bc.machlock.Lock()
	defer bc.machlock.Unlock()
	for k, v := range bc.machmap {
		ret[k] = v.JournalOutput()
	}
	return ret
}
