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

// Package byon ("bring your own node") runs kola tests against a pre-existing,
// already-booted machine instead of provisioning one. It ignores test-supplied
// Ignition/userdata, connects via a caller-supplied SSH user+key the node
// already trusts, and never creates or destroys the node. Only tests
// that don't rely on provisioning are meaningful here.
package byon

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/coreos/pkg/capnslog"
	"golang.org/x/crypto/ssh"

	ctplatform "github.com/flatcar/container-linux-config-transpiler/config/platform"
	"github.com/flatcar/mantle/network"
	"github.com/flatcar/mantle/platform"
)

const (
	Platform platform.Name = "byon"
)

var (
	plog = capnslog.NewPackageLogger("github.com/flatcar/mantle", "platform/machine/byon")
)

// Options contains BYON-specific options.
type Options struct {
	*platform.Options

	// User is the SSH user used to connect to the pre-existing node.
	User string
	// SSHKeyFile is the path to a private SSH key already authorized on the node.
	SSHKeyFile string
	// Node is the pre-existing node, given as HOST[:PORT] (PORT defaults to 22).
	Node string
}

// nodeSpec describes a single pre-existing machine.
type nodeSpec struct {
	addr string // host:port that kola dials
}

type flight struct {
	*platform.BaseFlight
	opts   *Options
	signer ssh.Signer

	mu   sync.Mutex
	free []nodeSpec
}

func loadSigner(path string) (ssh.Signer, error) {
	if path == "" {
		return nil, fmt.Errorf("a private SSH key is required (--byon-ssh-key)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading SSH key %q: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key %q: %w", path, err)
	}
	return signer, nil
}

// hostOnly returns the host part of a host:port address, or the input
// unchanged if it has no port.
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func parseNode(spec string) (nodeSpec, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nodeSpec{}, fmt.Errorf("a node address is required (--byon-node)")
	}

	return nodeSpec{addr: network.EnsurePortSuffix(spec, 22)}, nil
}

func NewFlight(opts *Options) (platform.Flight, error) {
	if opts.User == "" {
		return nil, fmt.Errorf("an SSH user is required (--byon-user)")
	}
	signer, err := loadSigner(opts.SSHKeyFile)
	if err != nil {
		return nil, err
	}
	node, err := parseNode(opts.Node)
	if err != nil {
		return nil, err
	}

	base, err := platform.NewBaseFlight(opts.Options, Platform, ctplatform.Custom)
	if err != nil {
		return nil, err
	}

	bf := &flight{
		BaseFlight: base,
		opts:       opts,
		signer:     signer,
		free:       []nodeSpec{node},
	}

	return bf, nil
}

// acquire removes and returns n nodes from the free pool, or errors if the
// pool does not hold that many.
// note: currently only one node is supported.
func (bf *flight) acquire(n int) ([]nodeSpec, error) {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	if len(bf.free) < n {
		return nil, fmt.Errorf("requested %d node(s) but only %d available in the BYON pool; reduce --parallel/cluster size or provide more nodes", n, len(bf.free))
	}
	out := make([]nodeSpec, n)
	copy(out, bf.free[:n])
	bf.free = bf.free[n:]
	return out, nil
}

// release returns nodes to the free pool so later tests can reuse them.
func (bf *flight) release(specs ...nodeSpec) {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.free = append(bf.free, specs...)
}

func (bf *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	base, err := platform.NewBaseCluster(bf.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	bc := &cluster{
		BaseCluster: base,
		flight:      bf,
	}

	bf.AddCluster(bc)

	return bc, nil
}

func (bf *flight) Destroy() {
	// BaseFlight.Destroy destroys clusters (releasing nodes and tearing down
	// journals) and closes the SSH agent.
	bf.BaseFlight.Destroy()
}
