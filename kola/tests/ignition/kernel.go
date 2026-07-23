// Copyright The Mantle Authors
// SPDX-License-Identifier: Apache-2.0
package ignition

import (
	"github.com/coreos/go-semver/semver"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"github.com/flatcar/mantle/platform/conf"
)

func init() {
	register.Register(&register.Test{
		Name: "cl.ignition.kargs",
		// Ignition kernel_arguments injection is grub.cfg-based. It applies
		// on CL and on ACL-GRUB (both boot via grub.cfg), but not on
		// ACL-UKI (systemd-boot/UKI has no grub.cfg to inject into) — that
		// path self-skips at runtime below.
		Distros:     []string{"acl", "cl"},
		Run:         check,
		ClusterSize: 1,
		UserData: conf.Butane(`---
variant: flatcar
version: 1.0.0
kernel_arguments:
  should_exist:
    - quiet`),
		MinVersion: semver.Version{Major: 3185},
	})
}

func check(c cluster.TestCluster) {
	m := c.Machines()[0]

	// UKI-booted images have no grub.cfg for ignition to inject kernel args
	// into; the karg mechanism differs entirely (UKI addons). Skip here
	// rather than gating on Distros so ACL-GRUB still gets coverage.
	if _, err := c.SSH(m, "sudo test -d /boot/EFI/Linux"); err == nil {
		c.Skip("ignition kernel_arguments injection is grub.cfg-based; not applicable on a UKI-booted image")
	}

	c.AssertCmdOutputContains(m, "cat /proc/cmdline", " quiet") // assuming space for word separation
}
