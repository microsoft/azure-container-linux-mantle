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
	"context"
	"os"
	"path/filepath"

	"github.com/flatcar/mantle/platform"
	"github.com/flatcar/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	flight *flight
}

func (bc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	if userdata != nil && !userdata.IsEmpty() {
		plog.Warningf("byon platform ignores test userdata/Ignition; the node is pre-existing and is not re-provisioned")
	}

	specs, err := bc.flight.acquire(1)
	if err != nil {
		return nil, err
	}

	bm := &machine{
		cluster: bc,
		spec:    specs[0],
	}

	dir := filepath.Join(bc.RuntimeConf().OutputDir, bm.ID())
	if err := os.MkdirAll(dir, 0777); err != nil {
		bc.flight.release(specs...)
		return nil, err
	}

	if bm.journal, err = platform.NewJournal(dir); err != nil {
		bm.Destroy()
		return nil, err
	}

	plog.Infof("Using pre-existing machine %v", bm.ID())

	// Custom start sequence: start the journal and run basic health checks,
	// but deliberately skip platform.StartMachine, which would mutate the
	// node's SELinux mode and audit rules.
	if err := bm.journal.Start(context.TODO(), bm); err != nil {
		bm.Destroy()
		return nil, err
	}
	if err := platform.CheckMachine(context.TODO(), bm); err != nil {
		bm.Destroy()
		return nil, err
	}

	bc.AddMach(bm)

	return bm, nil
}

func (bc *cluster) Destroy() {
	bc.BaseCluster.Destroy()
	bc.flight.DelCluster(bc)
}
