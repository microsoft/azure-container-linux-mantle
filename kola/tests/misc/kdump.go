// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0
package misc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"github.com/flatcar/mantle/platform"
	"github.com/flatcar/mantle/platform/conf"
)

func init() {
	// UKI boot mode: kdump is opt-in via the kdump.addon.efi UKI addon.
	// The addon appends crashkernel=256M to the kernel cmdline when placed
	// in the UKI's .extra.d directory. Without Secure Boot (qemu default),
	// the addon loads regardless of signature.
	register.Register(&register.Test{
		Run:         kdumpUKITest,
		ClusterSize: 1,
		Name:        "acl.kdump",
		// NoKernelPanicCheck: test intentionally triggers a panic.
		// NoEmergencyShellCheck: post-crash reboot may leave boot.mount dirty in journal.
		Flags:      []register.Flag{register.NoKernelPanicCheck, register.NoEmergencyShellCheck},
		MinVersion: semver.Version{Major: 3},
		// Temporarily disabled on ACL (bug 22249): when the machine does not come
		// back after the crash dump, the test only fails once the global kola
		// timeout elapses (~1h), so retries push the whole run past its timeout.
		// Drop ExcludeDistros once the post-crash wait has a bounded timeout.
		Distros:        []string{"acl"},
		ExcludeDistros: []string{"acl"},
		Platforms:      []string{"qemu", "qemu-unpriv", "azure"},
		UserData: conf.Butane(`---
version: 1.0.0
variant: flatcar
storage:
  files:
    - path: /opt/acl-activate-kdump
      mode: 0755
      contents:
        inline: |
          #!/bin/bash
          set -euo pipefail
          TEMPLATE="/boot/acl/uki-addons/kdump.addon.efi"
          # Discover the UKI name to find the correct .extra.d directory
          UKI_NAME="acl.efi"
          UKI_CANDIDATES=(/boot/EFI/Linux/vmlinuz-*.efi)
          if [[ -e "${UKI_CANDIDATES[0]}" ]]; then
            UKI_NAME=$(basename "${UKI_CANDIDATES[0]}")
          fi
          ADDON_DIR="/boot/EFI/Linux/${UKI_NAME}.extra.d"
          if [[ -f "${TEMPLATE}" ]] && [[ ! -f "${ADDON_DIR}/kdump.addon.efi" ]]; then
            mkdir -p "${ADDON_DIR}"
            cp "${TEMPLATE}" "${ADDON_DIR}/kdump.addon.efi"
            sync
            systemctl reboot --force
          else
            echo "kdump addon activation skipped: template=${TEMPLATE} exists=$(test -f "${TEMPLATE}" && echo y || echo n), addon already present=$(test -f "${ADDON_DIR}/kdump.addon.efi" && echo y || echo n)" >&2
            exit 1
          fi
systemd:
  units:
    - name: acl-activate-kdump.service
      enabled: true
      contents: |
        [Unit]
        Description=Activate kdump addon for UKI (one-shot on first boot)
        DefaultDependencies=no
        After=local-fs.target
        Before=basic.target
        RequiresMountsFor=/boot
        ConditionKernelCommandLine=!crashkernel

        [Service]
        Type=oneshot
        ExecStart=/opt/acl-activate-kdump

        [Install]
        WantedBy=sysinit.target`),
	})

	// GRUB boot mode: crashkernel= is delivered via the OEM grub.cfg(linux_append variable).
	// Azure excluded: writing /oem/grub.cfg does not persist across reboot on cloud platforms.
	register.Register(&register.Test{
		Run:         kdumpGRUBTest,
		ClusterSize: 1,
		Name:        "acl.kdump.grub",
		// NoKernelPanicCheck: test intentionally triggers a panic.
		// NoEmergencyShellCheck: post-crash reboot may leave boot.mount dirty in journal.
		Flags:      []register.Flag{register.NoKernelPanicCheck, register.NoEmergencyShellCheck},
		MinVersion: semver.Version{Major: 3},
		// Temporarily disabled on ACL (bug 22249): see acl.kdump above.
		Distros:        []string{"acl"},
		ExcludeDistros: []string{"acl"},
		Platforms:      []string{"qemu", "qemu-unpriv"},
	})
}

// kdumpUKITest validates kdump end-to-end on UKI boot: enable addon, reboot,
// assert crashkernel reserved, trigger panic, verify vmcore captured.
func kdumpUKITest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// kdump (kexec-tools) must be present in all ACL images
	if _, err := c.SSH(m, "test -f /usr/sbin/kdumpctl || test -f /usr/bin/kdumpctl"); err != nil {
		c.Fatalf("kdump (kexec-tools) not installed on this image")
	}

	// UKI test only - fail on GRUB-booted images
	if _, err := c.SSH(m, "sudo test -d /boot/EFI/Linux"); err != nil {
		c.Fatalf("UKI kdump test running on a GRUB-booted image")
	}

	// UKI test requires the addon template on the ESP (vfat is mounted umask=0077, needs root)
	c.MustSSH(m, "sudo test -f /boot/acl/uki-addons/kdump.addon.efi")

	kdumpVerifyAndCrash(c)
}

// kdumpGRUBTest validates kdump end-to-end on GRUB boot: inject crashkernel=
// via OEM grub.cfg, reboot, assert crashkernel reserved, trigger panic,
// verify vmcore captured.
func kdumpGRUBTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// kdump (kexec-tools) must be present in all ACL images
	if _, err := c.SSH(m, "test -f /usr/sbin/kdumpctl || test -f /usr/bin/kdumpctl"); err != nil {
		c.Fatalf("kdump (kexec-tools) not installed on this image")
	}

	// GRUB test only - fail on UKI-booted images
	if _, err := c.SSH(m, "sudo test -d /boot/EFI/Linux"); err == nil {
		c.Fatalf("GRUB kdump test running on a UKI-booted image")
	}

	// Append crashkernel= to OEM grub.cfg (preserving existing console= etc.)
	// aarch64 needs 512M (uncompressed kernel + makedumpfile runtime).
	arch := string(c.MustSSH(m, "uname -m"))
	crashkernelSize := "256M"
	if strings.TrimSpace(arch) == "aarch64" {
		crashkernelSize = "512M"
	}
	// Read existing linux_append (if any), append crashkernel, rewrite.
	c.MustSSH(m, fmt.Sprintf(`sudo bash -c '
		existing=""
		if [ -f /oem/grub.cfg ]; then
			existing=$(sed -n "s/^set linux_append=\"\([^\"]*\)\".*/\1/p" /oem/grub.cfg | head -n 1)
		fi
		echo "set linux_append=\"${existing} crashkernel=%s\"" | sudo tee /oem/grub.cfg > /dev/null
	'`, crashkernelSize))
	if err := m.Reboot(); err != nil {
		c.Fatalf("Failed to reboot after writing OEM grub.cfg: %v", err)
	}

	kdumpVerifyAndCrash(c)
}

// kdumpVerifyAndCrash is the shared verification + crash logic for both
// UKI and GRUB kdump tests.
func kdumpVerifyAndCrash(c cluster.TestCluster) {
	m := c.Machines()[0]

	// 1. Verify crashkernel= is present in /proc/cmdline
	cmdline := string(c.MustSSH(m, "cat /proc/cmdline"))
	if !strings.Contains(cmdline, "crashkernel=") {
		c.Fatalf("Expected crashkernel= in /proc/cmdline, got: %s", cmdline)
	}
	c.Logf("OK: /proc/cmdline contains crashkernel=")

	// 2. Verify kexec_crash_size > 0 (crash kernel memory reserved)
	crashSize := strings.TrimSpace(string(c.MustSSH(m, "cat /sys/kernel/kexec_crash_size")))
	if crashSize == "0" {
		c.Fatalf("Expected /sys/kernel/kexec_crash_size > 0, got 0")
	}
	c.Logf("OK: kexec_crash_size = %s", crashSize)

	// 3. Verify kdump.service is active (armed and ready)
	c.MustSSH(m, "systemctl is-active kdump.service")
	c.Logf("OK: kdump.service is active")

	// 4. Ensure sysrq trigger is enabled and trigger a kernel panic.
	// The machine will crash, kexec into the capture kernel, write a
	// vmcore to /var/crash, and reboot.
	c.MustSSH(m, "echo 1 | sudo tee /proc/sys/kernel/sysrq")

	c.Logf("Triggering kernel crash via sysrq-trigger...")
	// SSH will disconnect — the machine panics immediately
	_, _ = c.SSH(m, "sync; echo c | sudo tee /proc/sysrq-trigger")

	// Wait for the machine to come back after crash dump + reboot.
	// The full cycle (panic -> kexec -> capture vmcore -> reboot -> network up)
	// takes ~30s on amd64/QEMU but several minutes on aarch64.
	// Give time for the dump to complete before checking SSH.
	c.Logf("Waiting for VM to complete crash dump and reboot...")
	time.Sleep(30 * time.Second)

	// CheckMachine already retries SSH (SSHRetries x SSHTimeout approx= 10 min).
	if err := platform.CheckMachine(context.TODO(), m); err != nil {
		c.Fatalf("Machine did not come back after crash: %v", err)
	}
	c.Logf("VM back after crash dump + reboot")

	// 5. Verify a vmcore was captured in /var/crash (root-owned, needs sudo)
	output := string(c.MustSSH(m, "sudo find /var/crash -name 'vmcore' -type f 2>/dev/null | head -5"))
	if strings.TrimSpace(output) == "" {
		other := string(c.MustSSH(m, "sudo find /var/crash -name 'vmcore*' -o -name 'dump.*' -o -name 'dmesg.*' 2>/dev/null | head -5 || true"))
		diag := string(c.MustSSH(m, "sudo ls -laR /var/crash/ 2>/dev/null || true"))
		c.Fatalf("No vmcore found in /var/crash after crash.\nOther crash artifacts:\n%s\nContents:\n%s", other, diag)
	}
	vmcore := strings.Split(strings.TrimSpace(output), "\n")[0]
	c.Logf("OK: vmcore captured: %s", vmcore)

	// 6. Validate the dump is readable via makedumpfile (use the binary vmcore path)
	c.MustSSH(m, fmt.Sprintf("sudo makedumpfile --dump-dmesg %q /tmp/dmesg-from-vmcore.log", vmcore))
	c.Logf("OK: makedumpfile successfully extracted dmesg from vmcore")
}
