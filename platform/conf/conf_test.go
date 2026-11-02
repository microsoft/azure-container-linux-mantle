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

package conf

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/flatcar/mantle/network"
)

func TestConfAddSystemdUnitDropin(t *testing.T) {
	for _, version := range []string{"1", "2.0.0", "2.1.0", "2.2.0", "2.3.0", "3.0.0", "3.1.0", "3.2.0", "3.3.0"} {
		t.Run(version, func(t *testing.T) {
			data := `{"ignition":{"version":"` + version + `"}}`
			if version == "1" {
				data = `{"ignitionVersion":1}`
			}
			c, err := Ignition(data).Render("")
			if err != nil {
				t.Fatal(err)
			}
			const contents = "[Service]\nEnvironment=KOLA_TEST=1\n"
			c.AddSystemdUnit("example.service", "[Service]\nExecStart=/bin/true\n", true)
			c.AddSystemdUnitDropin("example.service", "10-existing.conf", contents)
			c.AddSystemdUnitDropin("other.service", "20-new.conf", contents)
			var config struct {
				Systemd struct {
					Units []struct {
						Name    string
						Dropins []struct {
							Name     string
							Contents string
						}
					}
				}
			}
			if err := json.Unmarshal(c.Bytes(), &config); err != nil {
				t.Fatal(err)
			}
			if len(config.Systemd.Units) != 2 {
				t.Fatalf("expected two units, got %s", c.String())
			}
			for i, name := range []string{"10-existing.conf", "20-new.conf"} {
				dropins := config.Systemd.Units[i].Dropins
				if len(dropins) != 1 || dropins[0].Name != name || dropins[0].Contents != contents {
					t.Errorf("unexpected dropins for %s: %+v", config.Systemd.Units[i].Name, dropins)
				}
			}
		})
	}
}

func TestConfCopyKey(t *testing.T) {
	agent, err := network.NewSSHAgent(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewSSHAgent failed: %v", err)
	}

	keys, err := agent.List()
	if err != nil {
		t.Fatalf("agent.List failed: %v", err)
	}

	tests := []*UserData{
		ContainerLinuxConfig(""),
		Ignition(`{ "ignition": { "version": "2.2.0" } }`),
		Ignition(`{ "ignition": { "version": "2.1.0" } }`),
		Ignition(`{ "ignition": { "version": "2.0.0" } }`),
		Ignition(`{ "ignition": { "version": "3.0.0" } }`),
		Ignition(`{ "ignition": { "version": "3.1.0" } }`),
		Ignition(`{ "ignition": { "version": "3.2.0" } }`),
		Ignition(`{ "ignition": { "version": "3.3.0" } }`),
		Ignition(`{ "ignitionVersion": 1 }`),
		CloudConfig("#cloud-config"),
		Butane("variant: flatcar\nversion: 1.0.0"),
	}

	for i, tt := range tests {
		conf, err := tt.Render("")
		if err != nil {
			t.Errorf("failed to parse config %d: %v", i, err)
			continue
		}

		conf.CopyKeys(keys)

		str := conf.String()

		if !strings.Contains(str, "ssh-rsa ") || !strings.Contains(str, " core@default") {
			t.Errorf("ssh public key not found in config %d: %s", i, str)
			continue
		}
	}
}

func TestConfAddUserToGroups(t *testing.T) {
	tests := []struct {
		u *UserData
		e error
	}{
		{
			CloudConfig("#cloud-config"),
			errors.New("missing addUserToGroups implementation for this config type"),
		},
		{
			Ignition(`{ "ignitionVersion": 1 }`),
			errors.New("missing addUserToGroups implementation for this config type"),
		},
		{
			Ignition(`{ "ignition": { "version": "2.2.0" } }`),
			errors.New("missing addUserToGroups implementation for this config type"),
		},
		{
			Ignition(`{ "ignition": { "version": "2.1.0" } }`),
			errors.New("missing addUserToGroups implementation for this config type"),
		},
		{
			Ignition(`{ "ignition": { "version": "2.0.0" } }`),
			errors.New("missing addUserToGroups implementation for this config type"),
		},
		{
			Ignition(`{ "ignition": { "version": "3.0.0" } }`),
			nil,
		},
		{
			Ignition(`{ "ignition": { "version": "3.1.0" } }`),
			nil,
		},
		{
			Ignition(`{ "ignition": { "version": "3.2.0" } }`),
			nil,
		},
		{
			Ignition(`{ "ignition": { "version": "3.3.0" } }`),
			nil,
		},
		{
			Butane("variant: flatcar\nversion: 1.0.0"),
			nil,
		},
	}

	for i, tt := range tests {
		conf, err := tt.u.Render("")
		if err != nil {
			t.Errorf("failed to parse config %d: %v", i, err)
			continue
		}

		err = conf.AddUserToGroups("test", []string{"sudo"})
		if tt.e == nil && err != nil {
			t.Errorf("should get nil error, got: %v", err)
		} else if tt.e != nil && err == nil {
			t.Errorf("should get an error, got a nil error")
		}
	}
}
