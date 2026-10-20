package platform

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"
)

type readinessReply struct {
	command string
	out     string
	err     error
}

type readinessMachine struct {
	Machine
	replies []readinessReply
	calls   int
}

func (m *readinessMachine) RuntimeConf() *RuntimeConfig {
	return &RuntimeConfig{SSHRetries: 3, SSHTimeout: time.Millisecond}
}

func (m *readinessMachine) SSH(command string) ([]byte, []byte, error) {
	if m.calls >= len(m.replies) {
		return nil, nil, fmt.Errorf("unexpected SSH command %q", command)
	}
	reply := m.replies[m.calls]
	m.calls++
	if command != reply.command {
		return nil, nil, fmt.Errorf("got SSH command %q, want %q", command, reply.command)
	}
	return []byte(reply.out), nil, reply.err
}

func TestCheckMachineWaitsForUsableSSH(t *testing.T) {
	m := &readinessMachine{replies: []readinessReply{
		{"systemctl is-system-running", "This account is currently not available.", errors.New("exit status 1")},
		{"systemctl is-system-running", "starting", errors.New("exit status 1")},
		{"systemctl list-jobs", "kola-core-setup.service start running", nil},
		{"systemctl is-system-running", "running", nil},
		{"grep ^ID= /etc/os-release", "ID=azurelinux", nil},
		{"systemctl --no-legend --state failed list-units", "", nil},
	}}
	if err := CheckMachine(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if m.calls != len(m.replies) {
		t.Fatalf("got %d SSH calls, want %d", m.calls, len(m.replies))
	}
}

func TestCheckMachineRejectsUnconfirmedReadiness(t *testing.T) {
	for _, reply := range []readinessReply{
		{"systemctl is-system-running", "This account is currently not available.", errors.New("exit status 1")},
		{"systemctl is-system-running", "unexpected response", nil},
		{"systemctl is-system-running", "", nil},
		{"systemctl is-system-running", "initializing", nil},
		{"systemctl is-system-running", "running", errors.New("connection lost")},
	} {
		t.Run(reply.out, func(t *testing.T) {
			m := &readinessMachine{replies: []readinessReply{reply, reply, reply}}
			err := CheckMachine(context.Background(), m)
			if err == nil || !strings.Contains(err.Error(), "ssh unreachable or system not ready") {
				t.Fatalf("expected readiness failure, got %v", err)
			}
			if m.calls != 3 {
				t.Fatalf("got %d probes, want 3", m.calls)
			}
		})
	}
}

func TestCheckMachineDegradedStillChecksFailedUnits(t *testing.T) {
	m := &readinessMachine{replies: []readinessReply{
		{"systemctl is-system-running", "degraded", errors.New("exit status 1")},
		{"grep ^ID= /etc/os-release", "ID=azurelinux", nil},
		{"systemctl --no-legend --state failed list-units", "broken.service loaded failed failed Broken", nil},
		{"journalctl -b -u broken.service", "failure details", nil},
		{"systemctl status broken.service", "failed", errors.New("exit status 3")},
	}}
	err := CheckMachine(context.Background(), m)
	if err == nil || !strings.Contains(err.Error(), "some systemd units failed") {
		t.Fatalf("expected failed-unit diagnostic, got %v", err)
	}
	if m.calls != len(m.replies) {
		t.Fatalf("got %d SSH calls, want %d", m.calls, len(m.replies))
	}
}
