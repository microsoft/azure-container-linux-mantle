// Copyright 2018 CoreOS, Inc.
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

package util

import (
	"fmt"

	"github.com/flatcar/mantle/kola"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/platform"
)

func AssertBootedUsr(c cluster.TestCluster, m platform.Machine, usr string) {
	usrdev := GetUsrDeviceNode(c, m)
	target := c.MustSSH(m, "readlink -f /dev/disk/by-partlabel/"+usr)
	if usrdev != string(target) {
		c.Fatalf("Expected /usr to be %v (%s) but it is %v", usr, target, usrdev)
	}
}

func GetUsrDeviceNode(c cluster.TestCluster, m platform.Machine) string {
	// The rootdev tool finds the backing block dev better than, e.g.,
	// findmnt -fno SOURCE /usr and/or dmsetup info --noheadings -Co blkdevs_used usr
	if kola.Options.Distribution != "acl" {
		return string(c.MustSSH(m, "rootdev -s /usr"))
	} else {
		// ACL doesn't include seismograph, which includes rootdev
		datadev := c.MustSSH(m, "sudo veritysetup status usr | grep 'data device' | cut -d ':' -f 2 | xargs")
		if len(datadev) == 0 {
			c.Fatalf("data device not found in veritysetup status output")
		}
		return string(datadev)

	}
}

func GetUsrHashDeviceNode(c cluster.TestCluster, m platform.Machine) string {
	if kola.Options.Distribution != "acl" {
		return GetUsrDeviceNode(c, m)
	} else {
		// ACL doesn't include seismograph, which includes rootdev
		hashdev := c.MustSSH(m, "sudo veritysetup status usr | grep 'hash device' | cut -d ':' -f 2 | xargs")
		if len(hashdev) == 0 {
			c.Fatalf("hash device not found in veritysetup status output")
		}
		return string(hashdev)
	}
}

func InvalidateUsrPartition(c cluster.TestCluster, m platform.Machine, partition string) {
	if out, stderr, err := m.SSH(fmt.Sprintf("sudo flatcar-setgoodroot && sudo wipefs /dev/disk/by-partlabel/%s", partition)); err != nil {
		c.Fatalf("invalidating %s failed: %s: %v: %s", partition, out, err, stderr)
	}
}
