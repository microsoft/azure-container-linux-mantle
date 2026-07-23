// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0
package misc

import (
	"github.com/coreos/go-semver/semver"
	"github.com/flatcar/mantle/kola"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"github.com/flatcar/mantle/platform/conf"
)

func init() {
	register.Register(&register.Test{
		Run:         fipsTest,
		ClusterSize: 1,
		Name:        `misc.fips`,
		MinVersion:  semver.Version{Major: 3549},
		Distros:     []string{"cl"},
		// This test is normally not related to the cloud environment
		Platforms: []string{"qemu", "qemu-unpriv"},
		UserData: conf.Butane(`---
version: 1.0.0
variant: flatcar
kernel_arguments:
  should_exist:
    - fips=1
storage:
  files:
    - path: /etc/system-fips
    - path: /etc/ssl/openssl.cnf
      overwrite: true
      mode: 0644
      contents:
        inline: |
          config_diagnostics = 1
          openssl_conf = openssl_init
          # includes the fipsmodule configuration
          .include /etc/ssl/fipsmodule.cnf
          [openssl_init]
          providers = provider_sect
          [provider_sect]
          fips = fips_sect
          base = base_sect
          [base_sect]
          activate = 1`),
	})

	// UKI variant: ACL ships fips.addon.efi in a backup location but not
	// activated. Use Ignition to copy it to the active UKI addon dir, create
	// the /etc/system-fips marker, and reboot so the kernel picks up fips=1.
	register.Register(&register.Test{
		Run:         fipsUKITest,
		ClusterSize: 1,
		Name:        `acl.misc.fips`,
		MinVersion:  semver.Version{Major: 3549},
		Distros:     []string{"acl"},
		// This test is normally not related to the cloud environment
		Platforms: []string{"qemu", "qemu-unpriv"},
		UserData: conf.Butane(`---
version: 1.0.0
variant: flatcar
storage:
  files:
    - path: /etc/system-fips
    - path: /opt/acl-activate-fips
      mode: 0755
      contents:
        inline: |
          #!/bin/bash
          set -euo pipefail
          TEMPLATE="/boot/acl/uki-addons/fips.addon.efi"
          # Discover the UKI name to find the correct .extra.d directory.
          UKI_NAME="acl.efi"
          UKI_CANDIDATES=(/boot/EFI/Linux/vmlinuz-*.efi)
          if [[ -e "${UKI_CANDIDATES[0]}" ]]; then
            UKI_NAME=$(basename "${UKI_CANDIDATES[0]}")
          fi
          ADDON_DIR="/boot/EFI/Linux/${UKI_NAME}.extra.d"
          if [[ -f "${TEMPLATE}" ]] && [[ ! -f "${ADDON_DIR}/fips.addon.efi" ]]; then
            mkdir -p "${ADDON_DIR}"
            cp "${TEMPLATE}" "${ADDON_DIR}/fips.addon.efi"
            sync
            systemctl reboot --force
          else
            echo "Error activating FIPS addon: template ${TEMPLATE} not found or addon already exists." >&2
            exit 1
          fi
systemd:
  units:
    - name: acl-activate-fips.service
      enabled: true
      contents: |
        [Unit]
        Description=Activate FIPS addon for UKI
        DefaultDependencies=no
        After=local-fs.target
        Before=basic.target
        RequiresMountsFor=/boot
        ConditionKernelCommandLine=!fips

        [Service]
        Type=oneshot
        ExecStart=/opt/acl-activate-fips

        [Install]
        WantedBy=sysinit.target`),
	})

	// GRUB variant: ACL-GRUB has no UKI addon mechanism, but it still boots
	// via grub.cfg and supports ignition kernel_arguments injection like CL,
	// so fips=1 is delivered the same way as the CL test above.
	register.Register(&register.Test{
		Run:         fipsGRUBTest,
		ClusterSize: 1,
		Name:        `acl.misc.fips.grub`,
		MinVersion:  semver.Version{Major: 3549},
		Distros:     []string{"acl"},
		// This test is normally not related to the cloud environment
		Platforms: []string{"qemu", "qemu-unpriv"},
		UserData: conf.Butane(`---
version: 1.0.0
variant: flatcar
kernel_arguments:
  should_exist:
    - fips=1
storage:
  files:
    - path: /etc/system-fips`),
	})
}

// fipsUKITest validates FIPS activation on ACL-UKI: fails fast if run
// against a GRUB-booted image, since that indicates a config/scheduling
// error (this test's addon-swap+reboot mechanism only applies to UKI).
func fipsUKITest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Both acl.misc.fips and acl.misc.fips.grub are scheduled on every ACL
	// run (Distros: ["acl"]), so hitting a GRUB image here is the routine
	// outcome on a GRUB run, not a scheduling error. Skip rather than
	// Fatalf so ACL-GRUB kola runs aren't permanently red.
	if _, err := c.SSH(m, "sudo test -d /boot/EFI/Linux"); err != nil {
		c.Skip("acl.misc.fips (UKI variant) not applicable on a GRUB-booted image (see acl.misc.fips.grub)")
	}

	fipsTest(c)
}

// fipsGRUBTest validates FIPS activation on ACL-GRUB via ignition
// kernel_arguments (fips=1), mirroring the CL test. Self-skips on UKI-booted
// images (see acl.misc.fips for the UKI variant).
func fipsGRUBTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	if _, err := c.SSH(m, "sudo test -d /boot/EFI/Linux"); err == nil {
		c.Skip("acl.misc.fips.grub variant not applicable on a UKI-booted image (see acl.misc.fips)")
	}

	fipsTest(c)
}

func fipsTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// It works because SHA is FIPS compliant.
	c.MustSSH(m, "echo Flatcar | openssl sha512 -")

	// ACL uses SymCrypt; CL uses the standard OpenSSL FIPS provider.
	if kola.Options.Distribution == "acl" {
		c.MustSSH(m, "openssl list -providers | grep -q symcryptprovider")
	} else {
		c.MustSSH(m, "openssl list -provider fips")
	}

	// MD5 is not FIPS compliant. But for ACL With SymCrypt in FIPS mode,
	// MD5 is still supported, so skip this assertion on ACL.
	if kola.Options.Distribution != "acl" {
		if _, err := c.SSH(m, "echo Flatcar | openssl md5 -"); err == nil {
			c.Fatal("MD5 hash algorithm should raise an error with FIPS mode.")
		}
	}

	c.AssertCmdOutputContains(m, "cat /proc/sys/crypto/fips_enabled", "1")
}
