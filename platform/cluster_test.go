// Copyright The Mantle Authors
// SPDX-License-Identifier: Apache-2.0

package platform

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/flatcar/mantle/platform/conf"
)

type renderedUnit struct {
	Name     string
	Contents string
	Dropins  []struct {
		Name     string
		Contents string
	}
}

func renderClusterUnits(t *testing.T, distro, user, ignitionVersion string, userdata *conf.UserData) []renderedUnit {
	t.Helper()
	rendered := renderClusterConfig(t, distro, user, ignitionVersion, userdata)
	var document struct {
		Systemd struct {
			Units []renderedUnit
		}
	}
	if err := json.Unmarshal(rendered.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	return document.Systemd.Units
}

func renderClusterConfig(t *testing.T, distro, user, ignitionVersion string, userdata *conf.UserData) *conf.Conf {
	t.Helper()
	flight := &BaseFlight{
		baseopts:   &Options{Distribution: distro, IgnitionVersion: ignitionVersion},
		ctPlatform: "custom",
	}
	cluster, err := NewBaseCluster(flight, &RuntimeConfig{DefaultUser: user, NoSSHKeyInUserData: true})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := cluster.RenderUserData(userdata, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.IsIgnition() && !rendered.ValidConfig() {
		t.Fatal("rendered configuration is invalid")
	}
	return rendered
}

func TestACLCoreSetupOrdersSSH(t *testing.T) {
	inputs := map[string]*conf.UserData{
		"default":     nil,
		"ignition-v1": conf.Ignition(`{"ignitionVersion":1}`),
		"clc":         conf.ContainerLinuxConfig(""),
		"butane":      conf.Butane("variant: flatcar\nversion: 1.0.0\n"),
	}
	for _, version := range []string{"2.0.0", "2.1.0", "2.2.0", "2.3.0", "3.0.0", "3.1.0", "3.2.0", "3.3.0"} {
		inputs[version] = conf.Ignition(fmt.Sprintf(`{"ignition":{"version":%q}}`, version))
	}
	for name, userdata := range inputs {
		t.Run(name, func(t *testing.T) {
			units := renderClusterUnits(t, "acl", "", "v3", userdata)
			found := map[string]bool{}
			for _, unit := range units {
				if unit.Name == "kola-core-setup.service" {
					found[unit.Name] = true
					if !strings.Contains(unit.Contents, "usermod -s /bin/bash") {
						t.Error("the existing core-user setup must be retained")
					}
					if strings.Contains(unit.Contents, "Before=sshd.socket") {
						t.Error("ordering setup before sockets can create a basic.target cycle")
					}
				}
				for _, dropin := range unit.Dropins {
					if dropin.Name != "10-kola-core-setup.conf" {
						continue
					}
					if unit.Name != "sshd.service" && unit.Name != "sshd@.service" {
						t.Errorf("unexpected SSH setup dependency on %s", unit.Name)
					}
					for _, directive := range []string{"[Unit]", "Requires=kola-core-setup.service", "After=kola-core-setup.service"} {
						if !strings.Contains(dropin.Contents, directive+"\n") {
							t.Errorf("%s drop-in is missing %s", unit.Name, directive)
						}
					}
					found[unit.Name] = true
				}
			}
			if !found["kola-core-setup.service"] {
				t.Error("the core-user setup unit is missing")
			}
			for _, service := range []string{"sshd.service", "sshd@.service"} {
				if !found[service] {
					t.Errorf("%s can start before the core test account is ready", service)
				}
			}
		})
	}
}

func TestACLCoreSetupPreservesExistingSSHConfig(t *testing.T) {
	userdata := conf.Ignition(`{"ignition":{"version":"3.3.0"},"systemd":{"units":[{"name":"sshd@.service","dropins":[{"name":"90-test.conf","contents":"[Service]\nEnvironment=TEST=kept\n"}]}]}}`)
	units := renderClusterUnits(t, "acl", "core", "v3", userdata)
	for _, unit := range units {
		if unit.Name != "sshd@.service" {
			continue
		}
		found := false
		for _, dropin := range unit.Dropins {
			if dropin.Name == "90-test.conf" && dropin.Contents == "[Service]\nEnvironment=TEST=kept\n" {
				found = true
			}
		}
		if !found {
			t.Error("the test's existing SSH drop-in was changed")
		}
		if len(unit.Dropins) != 2 {
			t.Errorf("expected existing and setup drop-ins, got %d", len(unit.Dropins))
		}
		return
	}
	t.Fatal("sshd@.service configuration was lost")
}

func TestCoreSetupSSHDependencyIsACLOnly(t *testing.T) {
	for _, tc := range []struct{ distro, user string }{
		{"cl", ""}, {"cl", "core"}, {"fcos", "core"}, {"acl", "tester"},
	} {
		t.Run(tc.distro+"/"+tc.user, func(t *testing.T) {
			units := renderClusterUnits(t, tc.distro, tc.user, "v3", nil)
			for _, unit := range units {
				if unit.Name == "kola-core-setup.service" {
					t.Error("core setup must not be injected for this distro/user")
				}
				for _, dropin := range unit.Dropins {
					if strings.Contains(dropin.Contents, "kola-core-setup.service") {
						t.Errorf("unexpected test-account dependency on %s", unit.Name)
					}
				}
			}
		})
	}
}

func TestCoreSetupSSHDependencyIsIgnitionOnly(t *testing.T) {
	for name, userdata := range map[string]*conf.UserData{
		"cloud-config": conf.CloudConfig("#cloud-config\n"),
		"script":       conf.Script("#!/bin/bash\n"),
		"multipart": conf.MultipartMimeConfig("MIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=boundary\n\n" +
			"--boundary\nContent-Type: text/cloud-config\n\n#cloud-config\n--boundary--\n"),
	} {
		t.Run(name, func(t *testing.T) {
			rendered := renderClusterConfig(t, "acl", "core", "v3", userdata)
			if strings.Contains(rendered.String(), "10-kola-core-setup.conf") {
				t.Error("non-Ignition bootstrap must not gain SSH service dependencies")
			}
		})
	}
}
