// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0
package misc

import (
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/flatcar/mantle/kola"
	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
)

func init() {
	// UKI boot mode: kdump addon appends crashkernel=256M via the UKI
	// .extra.d mechanism. Loads without Secure Boot (qemu default).
	// aarch64 qemu skipped at runtime, TCG emulation too slow for crash dump.
	register.Register(&register.Test{
		Run:         kdumpUKITest,
		ClusterSize: 1,
		Name:        "acl.kdump",
		Flags:       []register.Flag{register.NoKernelPanicCheck, register.NoEmergencyShellCheck}, // intentional panic + dirty journal
		MinVersion:  semver.Version{Major: 3},
		Distros:     []string{"acl"},
		Platforms:   []string{"azure", "qemu"},
	})

	// GRUB boot mode: crashkernel= via OEM grub.cfg linux_append.
	// Azure excluded - /oem doesn't persist across reboot on cloud platforms.
	register.Register(&register.Test{
		Run:         kdumpGRUBTest,
		ClusterSize: 1,
		Name:        "acl.kdump.grub",
		Flags:       []register.Flag{register.NoKernelPanicCheck, register.NoEmergencyShellCheck}, // intentional panic + dirty journal
		MinVersion:  semver.Version{Major: 3},
		Distros:     []string{"acl"},
		Platforms:   []string{"qemu", "qemu-unpriv"},
	})
}

// kdumpUKITest validates kdump end-to-end on UKI boot: enable addon, reboot,
// assert crashkernel reserved, trigger panic, verify vmcore captured.
func kdumpUKITest(c cluster.TestCluster) {
	// aarch64 qemu (TCG emulation) is too slow for the crash dump cycle.
	if string(c.Platform()) == "qemu" && kola.QEMUOptions.Board == "arm64-usr" {
		c.Skip("crash dump cycle too slow on aarch64 qemu (TCG emulation)")
	}

	m := c.Machines()[0]

	// kdump (kexec-tools) must be present in all ACL images
	if _, err := c.SSH(m, "test -f /usr/sbin/kdumpctl || test -f /usr/bin/kdumpctl"); err != nil {
		c.Fatalf("kdump (kexec-tools) not installed on this image")
	}

	// UKI test only - skip on GRUB-booted images (not applicable, not a defect).
	if _, err := c.SSH(m, "sudo test -d /boot/EFI/Linux"); err != nil {
		c.Skip("UKI kdump test not applicable on a GRUB-booted image")
	}

	c.MustSSH(m, "sudo test -f /boot/acl/uki-addons/kdump.addon.efi")

	// Copy addon into the UKI .extra.d directory. Done via SSH rather than
	// a systemd service to avoid a reboot that races with kola's SSH check.
	c.MustSSH(m, `sudo bash -c '
		set -euo pipefail
		TEMPLATE="/boot/acl/uki-addons/kdump.addon.efi"
		UKI_NAME="acl.efi"
		UKI_CANDIDATES=(/boot/EFI/Linux/vmlinuz-*.efi)
		if [[ -e "${UKI_CANDIDATES[0]}" ]]; then
			UKI_NAME=$(basename "${UKI_CANDIDATES[0]}")
		fi
		ADDON_DIR="/boot/EFI/Linux/${UKI_NAME}.extra.d"
		mkdir -p "${ADDON_DIR}"
		cp "${TEMPLATE}" "${ADDON_DIR}/kdump.addon.efi"
		sync
	'`)
	c.Logf("kdump addon activated, rebooting to pick up crashkernel= cmdline")
	if err := m.Reboot(); err != nil {
		c.Fatalf("Failed to reboot after kdump addon activation: %v", err)
	}

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

	// GRUB test only - skip on UKI-booted images (not applicable, not a defect).
	if _, err := c.SSH(m, "sudo test -d /boot/EFI/Linux"); err == nil {
		c.Skip("GRUB kdump test not applicable on a UKI-booted image")
	}

	// Inject crashkernel= via OEM grub.cfg. aarch64 needs 512M.
	arch := string(c.MustSSH(m, "uname -m"))
	crashkernelSize := "256M"
	if strings.TrimSpace(arch) == "aarch64" {
		crashkernelSize = "512M"
	}

	c.MustSSH(m, fmt.Sprintf(`sudo bash -c '
		set -euo pipefail
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

	// 4. Trigger a kernel panic — machine will kexec, dump vmcore, and reboot.
	c.MustSSH(m, "echo 1 | sudo tee /proc/sys/kernel/sysrq")
	c.Logf("Triggering kernel crash via sysrq-trigger...")
	_, _ = c.SSH(m, "sync; echo c | sudo tee /proc/sysrq-trigger")

	// Wait for the machine to come back. The full crash-dump-reboot cycle
	// typically takes 2-3 min on amd64, aarch64 qemu is excluded via
	// kola_enforcing.yaml because TCG emulation is much slower.
	//
	// We avoid platform.CheckMachine here: its util.Retry(60, 10s) loop
	// can't interrupt an in-flight SSH call, and the SSH handshake has no
	// timeout (ssh.ClientConfig.Timeout is unset in network/ssh.go). If the
	// machine is half-alive (TCP accepts but sshd hangs), a single probe
	// blocks indefinitely. Each probe below gets a 45s hard deadline instead.
	c.Logf("Waiting for VM to complete crash dump and reboot...")
	start := time.Now()
	// Let the capture kernel finish before probing - early probes just
	// burn time on dial timeouts.
	time.Sleep(2 * time.Minute)
	var reconnected bool
	var lastErr error
	deadline := start.Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(15 * time.Second)
		// 45s hard cap per probe (dial + handshake + exec).
		done := make(chan error, 1)
		go func() { _, err := c.SSH(m, "true"); done <- err }()
		select {
		case err := <-done:
			lastErr = err
		case <-time.After(45 * time.Second):
			lastErr = fmt.Errorf("ssh probe timed out after 45s")
		}
		if lastErr == nil {
			reconnected = true
			break
		}
	}
	if !reconnected {
		c.Fatalf("Machine did not come back after crash (waited %v): %v", time.Since(start).Round(time.Second), lastErr)
	}
	c.Logf("VM back after crash dump + reboot")

	// 5. Verify vmcore was captured
	output := string(c.MustSSH(m, "sudo find /var/crash -name 'vmcore' -type f 2>/dev/null | head -5"))
	if strings.TrimSpace(output) == "" {
		other := string(c.MustSSH(m, "sudo find /var/crash -name 'vmcore*' -o -name 'dump.*' -o -name 'dmesg.*' 2>/dev/null | head -5 || true"))
		diag := string(c.MustSSH(m, "sudo ls -laR /var/crash/ 2>/dev/null || true"))
		c.Fatalf("No vmcore found in /var/crash after crash.\nOther crash artifacts:\n%s\nContents:\n%s", other, diag)
	}
	vmcore := strings.Split(strings.TrimSpace(output), "\n")[0]
	c.Logf("OK: vmcore captured: %s", vmcore)

	// 6. Validate the dump is readable via makedumpfile
	c.MustSSH(m, fmt.Sprintf("sudo makedumpfile --dump-dmesg %q /tmp/dmesg-from-vmcore.log", vmcore))
	c.Logf("OK: makedumpfile successfully extracted dmesg from vmcore")
}
