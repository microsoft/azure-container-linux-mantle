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
	"bytes"
	"fmt"
	"strings"

	"github.com/coreos/go-semver/semver"
	"github.com/flatcar/mantle/kola"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"github.com/flatcar/mantle/kola/tests/util"
	"github.com/flatcar/mantle/platform"
)

const (
	defaultUsrSize = "1065345024"
)

func init() {
	register.Register(&register.Test{
		Run:         Verity,
		ClusterSize: 1,
		Name:        "cl.verity",
		Distros:     []string{"acl", "cl"},
		// Somehow hangs
		ExcludePlatforms: []string{"qemu-unpriv"},
		Flags:            []register.Flag{register.NoKernelPanicCheck, register.NoVerityCorruptionCheck},
		MinVersion:       semver.Version{Major: 2943},
		// This test is normally not related to the cloud environment
		Platforms: []string{"qemu", "qemu-unpriv", "azure"},
		// Destructive: the corruption subtest bricks the node, so on byon the
		// harness must point it at a throwaway node, not a real one.
		SupportsByon: true,
	})
	// Non-destructive variant of cl.verity: skips VerityCorruption, which
	// panics the node's kernel, so it is safe on non-throwaway byon nodes.
	register.Register(&register.Test{
		Run:              VerityVerify,
		ClusterSize:      1,
		Name:             "cl.verity-verify",
		Distros:          []string{"acl", "cl"},
		ExcludePlatforms: []string{"qemu-unpriv"},
		MinVersion:       semver.Version{Major: 2943},
		// This test is normally not related to the cloud environment
		Platforms: []string{"qemu", "qemu-unpriv", "azure"},
		// Read-only hash check
		SupportsByon: true,
	})
}

func Verity(c cluster.TestCluster) {
	c.Run("verify", VerityVerify)
	// modifies disk; must run last
	c.Run("corruption", VerityCorruption)
}

// Verity verification tests.
// TODO(mischief): seems like a good candidate for kolet.

// VerityVerify asserts that the filesystem mounted on /usr matches the
// dm-verity root hash.
// On CL (Flatcar), a patched GRUB reads the hash from a fixed offset in the
// kernel binary. On ACL, the hash is passed via the kernel command line
// (usrhash= in grub.cfg or baked into the UKI), so we read it from /proc/cmdline.
func VerityVerify(c cluster.TestCluster) {
	m := c.Machines()[0]

	var hash []byte
	if kola.Options.Distribution == "acl" {
		// ACL passes the verity hash on the kernel command line
		hash = c.MustSSH(m, "sed -n 's/.*usrhash=\\([a-f0-9]*\\).*/\\1/p' /proc/cmdline")
		if len(hash) == 0 {
			c.Fatalf("usrhash= not found in /proc/cmdline")
		}
	} else {
		// CL embeds the hash in the kernel binary at a known offset
		rootOffset := getKernelVerityHashOffset(c)
		ddcmd := fmt.Sprintf("sudo dd if=/boot/flatcar/vmlinuz-a skip=%d count=64 bs=1 status=none", rootOffset)
		hash = c.MustSSH(m, ddcmd)
	}

	// find /usr dev
	usrdev := util.GetUsrDeviceNode(c, m)

	// figure out partition size for hash dev offset, fallback to the expected value in case e2size doesn't work
	var offset []byte
	if kola.Options.Distribution != "acl" {
		offset = c.MustSSH(m, "sudo e2size "+usrdev+" || echo "+defaultUsrSize)
	} else {
		// ACL doesn't include Seismograph, with e2size
		offset = c.MustSSH(m, "sudo btrfs inspect-internal dump-super "+usrdev+" | awk '/total_bytes/{print $2; exit}' || echo "+defaultUsrSize)
	}

	c.MustSSH(m, fmt.Sprintf("sudo veritysetup verify --verbose --hash-offset=%s %s %s %s", offset, usrdev, usrdev, hash))
}

// VerityCorruption asserts that a machine will fail to read a file from a
// verify filesystem whose blocks have been modified.
func VerityCorruption(c cluster.TestCluster) {
	m := c.Machines()[0]
	// skip unless we are actually using verity
	skipUnlessVerity(c, m)

	// assert that dm shows verity is in use and the device is valid (V)
	out := c.MustSSH(m, "sudo dmsetup --target verity status usr")

	fields := strings.Fields(string(out))
	if len(fields) != 4 {
		c.Fatalf("failed checking dmsetup status of usr: not enough fields in output (got %d)", len(fields))
	}

	if fields[3] != "V" {
		c.Fatalf("dmsetup status usr reports verity is not valid!")
	}

	// corrupt disk and flush disk caches.

	// get usr device, probably vda3
	usrdev := util.GetUsrDeviceNode(c, m)

	// write zero bytes to first 100 MB
	c.MustSSH(m, fmt.Sprintf(`sudo dd if=/dev/zero of=%s bs=1M count=100 status=none`, usrdev))

	// make sure we flush everything so the filesystem has to go through to the device backing verity before fetching a file from /usr
	// (done in one execution because after flushing command itself runs the corruption could already be detected,
	// we just need to give arm64 QEMU tests a few more chances to detect the corruption while one 'cat' execution is enough on amd64).
	_, err := c.SSH(m, "sudo /bin/sh -c 'sync; echo -n 3 >/proc/sys/vm/drop_caches; cat /usr/lib/os-release; ls -R /usr'")
	if err == nil {
		c.Fatalf("verity did not prevent reading from a corrupted disk (expected kernel panic)!")
	}
	if !strings.Contains(err.Error(), "wait: remote command exited without exit status or exit signal") {
		c.Fatalf("expected 'wait: remote command exited without exit status or exit signal' error due to kernel panic, got %v", err)
	}
	// machine will now reboot in a loop but never be reachable again because the only partition it has got corrupted
}

// get offset of verity hash within kernel
func getKernelVerityHashOffset(c cluster.TestCluster) int {
	// the QEMUOptions.Board is also used by other platforms
	if kola.QEMUOptions.Board == "arm64-usr" {
		return 512
	}
	return 64
}

func skipUnlessVerity(c cluster.TestCluster, m platform.Machine) {
	// figure out if we are actually using verity
	out, err := c.SSH(m, "sudo veritysetup status usr")
	if err != nil && bytes.Equal(out, []byte("/dev/mapper/usr is inactive.")) {
		// verity not in use, so skip.
		c.Skip("verity is not enabled")
	} else if err != nil {
		c.Fatalf("failed checking verity status: %s: %v", out, err)
	}
}
