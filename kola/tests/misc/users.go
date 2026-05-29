// Copyright 2016 CoreOS, Inc.
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

package misc

import (
	"github.com/flatcar/mantle/kola"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"strings"
)

func init() {
	register.Register(&register.Test{
		Run:              CheckUserShells,
		ClusterSize:      1,
		ExcludePlatforms: []string{"gce"},
		Name:             "cl.users.shells",
		Distros:          []string{"acl", "cl"},
		// This test is normally not related to the cloud environment
		Platforms: []string{"qemu", "qemu-unpriv", "azure"},
	})
}

func CheckUserShells(c cluster.TestCluster) {
	m := c.Machines()[0]
	var badusers []string

	ValidUsers := map[string]string{
		"root":     "/bin/bash",
		"sync":     "/bin/sync",
		"shutdown": "/sbin/shutdown",
		"halt":     "/sbin/halt",
		"core":     "/bin/bash",
	}

	output := c.MustSSH(m, "getent passwd")

	users := strings.Split(string(output), "\n")

	for _, user := range users {
		userdata := strings.Split(user, ":")
		if len(userdata) != 7 {
			badusers = append(badusers, user)
			continue
		}

		username := userdata[0]
		shell := userdata[6]
		if shell == "/bin/sh" {
			// gentent returns one entry for root with /bin/sh instead of /bin/bash
			// but /bin/sh is anyway a symlink to /bin/bash
			shell = "/bin/bash"
		}

		if kola.Options.Distribution == "acl" {
			// Accept common non-login shells across distros, ACL uses /bin/false.
			NonLoginShells := map[string]bool{
				"/sbin/nologin":     true,
				"/usr/sbin/nologin": true,
				"/usr/bin/nologin":  true,
				"/bin/false":        true,
				"/usr/bin/false":    true,
			}

			// Upstream logic, generalized for ACL:
			// flag only if shell is neither expected nor a non-login shell.
			if shell != ValidUsers[username] && !NonLoginShells[shell] {
				badusers = append(badusers, user)
			}
			continue
		}

		// Upstream logic.
		if shell != ValidUsers[username] && shell != "/sbin/nologin" {
			badusers = append(badusers, user)
		}
	}

	if len(badusers) != 0 {
		c.Fatalf("Invalid users: %v", badusers)
	}
}
