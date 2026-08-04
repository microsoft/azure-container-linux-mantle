// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package misc

import (
	"fmt"
	"strings"

	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
)

func init() {
	register.Register(&register.Test{
		Run:          CISSshd,
		ClusterSize:  1,
		Name:         "acl.security.cis.sshd",
		Distros:      []string{"acl"},
		Platforms:    []string{"qemu", "qemu-unpriv", "azure"},
		SupportsByon: true,
	})
	register.Register(&register.Test{
		Run:          CISModprobe,
		ClusterSize:  1,
		Name:         "acl.security.cis.modprobe",
		Distros:      []string{"acl"},
		Platforms:    []string{"qemu", "qemu-unpriv", "azure"},
		SupportsByon: true,
	})
	register.Register(&register.Test{
		Run:          CISLogPerms,
		ClusterSize:  1,
		Name:         "acl.security.cis.logperms",
		Distros:      []string{"acl"},
		Platforms:    []string{"qemu", "qemu-unpriv", "azure"},
		SupportsByon: true,
	})
}

// CISSshd checks that the CIS SSH hardening drop-in is active.
func CISSshd(c cluster.TestCluster) {
	m := c.Machines()[0]

	out := string(c.MustSSH(m, "sudo sshd -T"))
	lower := strings.ToLower(out)

	for _, want := range []string{
		"denyusers root",
		"denygroups root",
		"allowusers *",
		"allowgroups *",
		"maxauthtries 4",
		"disableforwarding yes",
	} {
		if !strings.Contains(lower, want) {
			c.Fatalf("sshd -T output missing %q; got:\n%s", want, out)
		}
	}

	// All sshd config files must be 0600
	for _, f := range []string{
		"/etc/ssh/sshd_config",
		"/etc/ssh/sshd_config.d/10-authorized-keys.conf",
		"/etc/ssh/sshd_config.d/50-acl-no-password-auth.conf",
		"/etc/ssh/sshd_config.d/60-acl-cis-hardening.conf",
	} {
		mode := strings.TrimSpace(string(c.MustSSH(m, fmt.Sprintf("stat -c '%%a' %s", f))))
		if mode != "600" {
			c.Fatalf("%s mode %s, want 600", f, mode)
		}
	}
}

// CISModprobe checks cramfs/sctp are blacklisted and masked.
func CISModprobe(c cluster.TestCluster) {
	m := c.Machines()[0]

	out := string(c.MustSSH(m, "cat /usr/lib/modprobe.d/cis-blacklist.conf"))
	for _, want := range []string{
		"install cramfs /bin/false",
		"blacklist cramfs",
		"install sctp /bin/true",
		"blacklist sctp",
	} {
		if !strings.Contains(out, want) {
			c.Fatalf("cis-blacklist.conf missing %q; got:\n%s", want, out)
		}
	}

	// sctp should be a silent no-op (exit 0), not a hard error
	c.MustSSH(m, "modprobe -n sctp")
}

// CISLogPerms checks /var/log/azure perms and waagent UMask.
func CISLogPerms(c cluster.TestCluster) {
	m := c.Machines()[0]

	mode := strings.TrimSpace(string(c.MustSSH(m, "stat -c '%a' /var/log/azure")))
	if mode != "750" {
		c.Fatalf("/var/log/azure mode %s, want 750", mode)
	}

	// waagent UMask drop-in only exists on Azure OEM (not qemu)
	if c.Platform() == "azure" {
		out := string(c.MustSSH(m, "cat /usr/lib/systemd/system/waagent.service.d/cis-umask.conf"))
		if !strings.Contains(out, "UMask=0027") {
			c.Fatalf("waagent cis-umask.conf missing UMask=0027; got:\n%s", out)
		}
	}
}
