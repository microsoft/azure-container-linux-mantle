// Copyright The Mantle Authors
// SPDX-License-Identifier: Apache-2.0

package platform

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/context"
)

type machineCheckReply struct {
	command string
	stdout  string
	stderr  string
	err     error
}

// Embedding Machine keeps this fake limited to the APIs CheckMachine uses.
type machineCheckFake struct {
	Machine
	t       *testing.T
	config  RuntimeConfig
	replies []machineCheckReply
}

func (m *machineCheckFake) RuntimeConf() *RuntimeConfig { return &m.config }

func (m *machineCheckFake) SSH(command string) ([]byte, []byte, error) {
	m.t.Helper()
	if len(m.replies) == 0 {
		m.t.Fatalf("unexpected SSH command: %s", command)
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	if command != reply.command {
		m.t.Fatalf("SSH command %q, expected %q", command, reply.command)
	}
	return []byte(reply.stdout), []byte(reply.stderr), reply.err
}

func TestCheckMachineWaitsForExecutableSSH(t *testing.T) {
	exitOne := errors.New("Process exited with status 1")
	for _, reply := range []machineCheckReply{
		{stdout: "This account is currently not available.", err: exitOne},
		{stderr: "ssh connection reset", err: errors.New("connection reset")},
		{stdout: "unexpected login banner"},
		{stdout: ""},
		{stdout: "start"},
		{stdout: "running", err: exitOne},
	} {
		t.Run(reply.stdout+reply.stderr, func(t *testing.T) {
			reply.command = "systemctl is-system-running"
			m := &machineCheckFake{t: t, config: RuntimeConfig{SSHRetries: 2}, replies: []machineCheckReply{
				reply,
				{command: "systemctl is-system-running", stdout: "running"},
				{command: "grep ^ID= /etc/os-release", stdout: "ID=azurelinux"},
				{command: "systemctl --no-legend --state failed list-units"},
			}}
			if err := CheckMachine(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			if len(m.replies) != 0 {
				t.Fatalf("%d expected SSH commands were not checked", len(m.replies))
			}
		})
	}
}

func TestCheckMachineReadinessFailureIsBounded(t *testing.T) {
	rejection := machineCheckReply{command: "systemctl is-system-running", stdout: "This account is currently not available.", err: errors.New("exit status 1")}
	m := &machineCheckFake{t: t, config: RuntimeConfig{SSHRetries: 3}, replies: []machineCheckReply{rejection, rejection, rejection}}
	err := CheckMachine(context.Background(), m)
	if err == nil || !strings.Contains(err.Error(), "ssh unreachable or system not ready") || !strings.Contains(err.Error(), rejection.stdout) {
		t.Fatalf("expected the actual account readiness error, got %v", err)
	}
	if len(m.replies) != 0 {
		t.Fatalf("expected exactly three readiness attempts, %d remain", len(m.replies))
	}
}

func TestCheckMachineWaitsWhileSystemStarts(t *testing.T) {
	for _, state := range []string{"initializing", "starting", "stopping"} {
		t.Run(state, func(t *testing.T) {
			replies := []machineCheckReply{{command: "systemctl is-system-running", stdout: state, err: errors.New("exit status 1")}}
			if state == "starting" {
				replies = append(replies, machineCheckReply{command: "systemctl list-jobs", stdout: "kola-core-setup.service start running"})
			}
			replies = append(replies,
				machineCheckReply{command: "systemctl is-system-running", stdout: "running"},
				machineCheckReply{command: "grep ^ID= /etc/os-release", stdout: "ID=flatcar"},
				machineCheckReply{command: "systemctl --no-legend --state failed list-units"},
			)
			m := &machineCheckFake{t: t, config: RuntimeConfig{SSHRetries: 2}, replies: replies}
			if err := CheckMachine(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			if len(m.replies) != 0 {
				t.Error("expected startup checks were skipped")
			}
		})
	}
}

func TestCheckMachinePreservesTerminalStateChecks(t *testing.T) {
	for _, state := range []string{"degraded", "maintenance", "offline", "unknown"} {
		for _, allowFailed := range []bool{false, true} {
			t.Run(state+"/"+map[bool]string{false: "reject failed units", true: "allow failed units"}[allowFailed], func(t *testing.T) {
				replies := []machineCheckReply{
					{command: "systemctl is-system-running", stdout: state, err: errors.New("exit status 1")},
					{command: "grep ^ID= /etc/os-release", stdout: "ID=azurelinux"},
				}
				if !allowFailed {
					replies = append(replies,
						machineCheckReply{command: "systemctl --no-legend --state failed list-units", stdout: "example.service loaded failed failed Example"},
						machineCheckReply{command: "journalctl -b -u example.service", stdout: "example failure journal"},
						machineCheckReply{command: "systemctl status example.service", stdout: "failed"},
					)
				}
				m := &machineCheckFake{t: t, config: RuntimeConfig{SSHRetries: 2, AllowFailedUnits: allowFailed}, replies: replies}
				err := CheckMachine(context.Background(), m)
				if allowFailed && err != nil {
					t.Fatal(err)
				}
				if !allowFailed && (err == nil || !strings.Contains(err.Error(), "some systemd units failed") || !strings.Contains(err.Error(), "example failure journal")) {
					t.Fatalf("expected the failed-unit diagnostics, got %v", err)
				}
				if len(m.replies) != 0 {
					t.Error("expected terminal-state checks were skipped")
				}
			})
		}
	}
}

func TestCheckMachineOSReleaseReportsCommandFailure(t *testing.T) {
	m := &machineCheckFake{t: t, config: RuntimeConfig{SSHRetries: 1}, replies: []machineCheckReply{
		{command: "systemctl is-system-running", stdout: "running"},
		{command: "grep ^ID= /etc/os-release", stdout: "This account is currently not available.", stderr: "diagnostic stderr", err: errors.New("exit status 1")},
	}}
	err := CheckMachine(context.Background(), m)
	if err == nil {
		t.Fatal("expected the OS identity command to fail")
	}
	for _, expected := range []string{"checking /etc/os-release", "This account is currently not available.", "diagnostic stderr", "exit status 1"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q is missing %q", err, expected)
		}
	}
	if strings.Contains(err.Error(), "no /etc/os-release file") {
		t.Error("a failed SSH command is not proof of a missing file")
	}
}

func TestCheckMachineRejectsWrongDistribution(t *testing.T) {
	m := &machineCheckFake{t: t, config: RuntimeConfig{SSHRetries: 1}, replies: []machineCheckReply{
		{command: "systemctl is-system-running", stdout: "running"},
		{command: "grep ^ID= /etc/os-release", stdout: "ID=other"},
	}}
	if err := CheckMachine(context.Background(), m); err == nil || !strings.Contains(err.Error(), "not a Flatcar Container Linux or Azure Container Linux instance") {
		t.Fatalf("wrong distro must still fail, got %v", err)
	}
}
