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

package byon

import (
	"bytes"

	"golang.org/x/crypto/ssh"

	"github.com/flatcar/mantle/platform"
)

type machine struct {
	cluster *cluster
	journal *platform.Journal
	spec    nodeSpec
}

func (bm *machine) ID() string {
	return bm.spec.addr
}

func (bm *machine) IP() string {
	return hostOnly(bm.spec.addr)
}

func (bm *machine) PrivateIP() string {
	return hostOnly(bm.spec.addr)
}

func (bm *machine) RuntimeConf() *platform.RuntimeConfig {
	return bm.cluster.RuntimeConf()
}

// sshConfig builds an SSH client config using the caller-supplied user and
// private key. The byon platform deliberately does not use the flight's
// ephemeral SSH agent, since a pre-existing node only trusts keys that were
// installed out of band.
func (bm *machine) sshConfig(user string, auth ...ssh.AuthMethod) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:    user,
		Auth:    auth,
		Timeout: bm.RuntimeConf().SSHTimeout,
		// TODO: optionally honor a caller-provided known_hosts file instead of
		// trusting any host key.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
}

func (bm *machine) SSHClient() (*ssh.Client, error) {
	cfg := bm.sshConfig(bm.cluster.flight.opts.User, ssh.PublicKeys(bm.cluster.flight.signer))
	return ssh.Dial("tcp", bm.spec.addr, cfg)
}

func (bm *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	cfg := bm.sshConfig(user, ssh.Password(password))
	return ssh.Dial("tcp", bm.spec.addr, cfg)
}

func (bm *machine) SSH(cmd string) ([]byte, []byte, error) {
	client, err := bm.SSHClient()
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	err = session.Run(cmd)
	return bytes.TrimSpace(stdout.Bytes()), bytes.TrimSpace(stderr.Bytes()), err
}

func (bm *machine) Reboot() error {
	return platform.RebootMachine(bm, bm.journal)
}

func (bm *machine) Destroy() {
	// Only tear down local bookkeeping and return the node to
	// the pool for reuse by later tests.
	if bm.journal != nil {
		bm.journal.Destroy()
	}
	bm.cluster.flight.release(bm.spec)
	bm.cluster.DelMach(bm)
}

func (bm *machine) ConsoleOutput() string {
	// No serial console is available for a pre-existing node.
	// Potentially add use user-provided scripts to capture console output later.
	return ""
}

func (bm *machine) JournalOutput() string {
	if bm.journal == nil {
		return ""
	}
	data, err := bm.journal.Read()
	if err != nil {
		plog.Errorf("Reading journal for machine %v: %v", bm.ID(), err)
	}
	return string(data)
}

func (bm *machine) Board() string {
	return bm.cluster.flight.Options().Board
}
