// Copyright 2017 CoreOS, Inc.
//
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

package misc

import (
	"fmt"

	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"github.com/flatcar/mantle/kola/tests/util"
	tutil "github.com/flatcar/mantle/kola/tests/util"
	"github.com/flatcar/mantle/platform"
	"github.com/flatcar/mantle/platform/conf"
)

const (
	IgnitionConfigRootRaid = `{
  "ignition": {
    "config": {},
    "security": {
      "tls": {}
    },
    "timeouts": {},
    "version": "2.3.0"
  },
  "networkd": {},
  "storage": {
    "disks": [
      {
        "device": "/dev/disk/by-id/virtio-secondary",
        "partitions": [
          {
            "label": "root1",
            "number": 1,
            "sizeMiB": 256,
            "typeGuid": "be9067b9-ea49-4f15-b4f6-f36f8c9e1818"
          },
          {
            "label": "root2",
            "number": 2,
            "sizeMiB": 256,
            "typeGuid": "be9067b9-ea49-4f15-b4f6-f36f8c9e1818"
          }
        ],
        "wipeTable": true
      }
    ],
    "filesystems": [
      {
        "mount": {
          "device": "/dev/md/rootarray",
          "format": "ext4",
          "label": "ROOT"
        },
        "name": "ROOT"
      },
      {
        "mount": {
          "device": "/dev/disk/by-partlabel/ROOT",
          "format": "ext4",
          "label": "wasteland",
          "wipeFilesystem": true
        },
        "name": "NOT_ROOT"
      }
    ],
    "raid": [
      {
        "devices": [
          "/dev/disk/by-partlabel/root1",
          "/dev/disk/by-partlabel/root2"
        ],
        "level": "{{ .RaidLevel }}",
        "name": "rootarray"
      }
    ]
  }
}
`

	IgnitionConfigDataRaid = `{
  "ignition": {
    "config": {},
    "security": {
      "tls": {}
    },
    "timeouts": {},
    "version": "2.3.0"
  },
  "networkd": {},
  "storage": {
    "disks": [
      {
        "device": "/dev/disk/by-partlabel/{{ .DataPartLabel }}"
      },
      {
        "device": "/dev/disk/by-partlabel/USR-B"
      }
    ],
    "filesystems": [
      {
        "name": "DATA",
        "mount": {
          "device": "/dev/md/DATA",
          "format": "ext4",
          "label": "DATA"
        }
      }
    ],
    "raid": [
      {
        "devices": [
          "/dev/disk/by-partlabel/{{ .DataPartLabel }}",
          "/dev/disk/by-partlabel/USR-B"
        ],
        "level": "{{ .RaidLevel }}",
        "name": "DATA"
      }
    ]
  },
  "systemd": {
    "units": [
      {
        "name": "var-lib-data.mount",
        "enabled": true,
        "contents": "[Mount]\nWhat=/dev/md/DATA\nWhere=/var/lib/data\nType=ext4\n\n[Install]\nWantedBy=local-fs.target"
      }
    ]
  }
}
`

	// IgnitionConfigDataRaidSecondaryDisk uses two partitions on a secondary
	// disk for the RAID array instead of repurposing existing primary-disk
	// partitions that may not be expendable in the UKI layout.
	IgnitionConfigDataRaidSecondaryDisk = `{
  "ignition": {
    "config": {},
    "security": {
      "tls": {}
    },
    "timeouts": {},
    "version": "2.3.0"
  },
  "networkd": {},
  "storage": {
    "disks": [
      {
        "device": "/dev/disk/by-id/virtio-secondary",
        "partitions": [
          {
            "label": "data1",
            "number": 1,
            "sizeMiB": 256,
            "typeGuid": "0fc63daf-8483-4772-8e79-3d69d8477de4"
          },
          {
            "label": "data2",
            "number": 2,
            "sizeMiB": 256,
            "typeGuid": "0fc63daf-8483-4772-8e79-3d69d8477de4"
          }
        ],
        "wipeTable": true
      }
    ],
    "filesystems": [
      {
        "name": "DATA",
        "mount": {
          "device": "/dev/md/DATA",
          "format": "ext4",
          "label": "DATA"
        }
      }
    ],
    "raid": [
      {
        "devices": [
          "/dev/disk/by-partlabel/data1",
          "/dev/disk/by-partlabel/data2"
        ],
        "level": "{{ .RaidLevel }}",
        "name": "DATA"
      }
    ]
  },
  "systemd": {
    "units": [
      {
        "name": "var-lib-data.mount",
        "enabled": true,
        "contents": "[Mount]\nWhat=/dev/md/DATA\nWhere=/var/lib/data\nType=ext4\n\n[Install]\nWantedBy=local-fs.target"
      }
    ]
  }
}
`
)

var (
	raidTypes = map[string]interface{}{
		"raid0": struct{}{},
		"raid1": struct{}{},
	}
)

type raidConfig struct {
	RaidLevel     string
	DataPartLabel string // "OEM-CONFIG" for CL (legacy layout) — unused for ACL
}

// distroLayout holds the partition references that differ between
// the legacy (CL) and UKI (ACL) disk layouts.
//
// The stale ROOT partition that the root-RAID test wipes and relabels
// ("wasteland") is now referenced by partlabel (/dev/disk/by-partlabel/ROOT)
// instead of a hardcoded partition number, since both layouts label it
// "ROOT" and the number has already shifted once (CL: part9 -> part11,
// ACL: part5 -> part7) as verity hash partitions were added ahead of it.
type distroLayout struct {
	prefix            string // test name prefix: "cl" or "acl"
	distro            string // distro tag for registration
	dataPartLabel     string // partlabel for the expendable data partition (CL only)
	dataUseSecondDisk bool   // if true, data RAID uses a secondary disk instead of primary-disk partitions
}

var distroLayouts = []distroLayout{
	{"cl", "cl", "OEM-CONFIG", false}, // legacy disk layout
	{"acl", "acl", "", true},          // UKI disk layout
}

func init() {
	for raidLevel := range raidTypes {
		level := raidLevel

		for _, dl := range distroLayouts {
			dl := dl // capture loop variable

			// root partition
			templRoot, err := util.ExecTemplate(IgnitionConfigRootRaid, raidConfig{
				RaidLevel: level,
			})
			if err != nil {
				fmt.Printf("fail to execute template for %s/%s: %v\n", dl.prefix, level, err)
				return
			}
			userDataRoot := conf.Ignition(templRoot)

			runRootOnRaid := func(c cluster.TestCluster) {
				RootOnRaid(c, userDataRoot)
			}

			register.Register(&register.Test{
				// This test needs additional disks which is only supported on qemu since Ignition
				// does not support deleting partitions without wiping the partition table and the
				// disk doesn't have room for new partitions.
				// TODO(ajeddeloh): change this to delete partition 9 and replace it with 9 and 10
				// once Ignition supports it.
				Run:         runRootOnRaid,
				ClusterSize: 0,
				// This test is normally not related to the cloud environment
				Platforms: []string{"qemu"},
				Name:      fmt.Sprintf("%s.disk.%s.root", dl.prefix, raidLevel),
				Distros:   []string{dl.distro},
			})

			// data partition
			if dl.dataUseSecondDisk {
				// ACL (UKI layout): primary-disk partitions like OEM-CONFIG
				// and USR-B are not expendable; use a secondary disk instead.
				templData, err := util.ExecTemplate(IgnitionConfigDataRaidSecondaryDisk, raidConfig{
					RaidLevel: level,
				})
				if err != nil {
					fmt.Printf("fail to execute template for %s/%s: %v\n", dl.prefix, level, err)
					return
				}
				userDataData := conf.Ignition(templData)

				runDataOnRaid := func(c cluster.TestCluster) {
					DataOnRaidSecondaryDisk(c, userDataData)
				}

				register.Register(&register.Test{
					Run:         runDataOnRaid,
					ClusterSize: 0,
					Name:        fmt.Sprintf("%s.disk.%s.data", dl.prefix, raidLevel),
					Distros:     []string{dl.distro},
					// Additional disks are only supported on qemu, not qemu-unpriv.
					Platforms: []string{"qemu"},
				})
			} else {
				// CL (legacy layout): repurpose OEM-CONFIG + USR-B on the primary disk
				templData, err := util.ExecTemplate(IgnitionConfigDataRaid, raidConfig{
					RaidLevel:     level,
					DataPartLabel: dl.dataPartLabel,
				})
				if err != nil {
					fmt.Printf("fail to execute template for %s/%s: %v\n", dl.prefix, level, err)
					return
				}
				userDataData := conf.Ignition(templData)

				runDataOnRaid := func(c cluster.TestCluster) {
					DataOnRaid(c, userDataData)
				}

				register.Register(&register.Test{
					Run:         runDataOnRaid,
					ClusterSize: 1,
					Name:        fmt.Sprintf("%s.disk.%s.data", dl.prefix, raidLevel),
					UserData:    userDataData,
					Distros:     []string{dl.distro},
					// This test is normally not related to the cloud environment
					Platforms: []string{"qemu", "qemu-unpriv"},
				})
			}
		}
	}
}

func RootOnRaid(c cluster.TestCluster, userData *conf.UserData) {
	options := platform.MachineOptions{
		AdditionalDisks: []platform.Disk{
			{Size: "520M", DeviceOpts: []string{"serial=secondary"}},
		},
	}
	m, err := tutil.NewMachineWithOptions(c, userData, options)
	if err != nil {
		c.Fatal(err)
	}

	checkIfMountpointIsRaid(c, m, "/")

	// reboot it to make sure it comes up again
	err = m.Reboot()
	if err != nil {
		c.Fatalf("could not reboot machine: %v", err)
	}

	checkIfMountpointIsRaid(c, m, "/")
}

func DataOnRaid(c cluster.TestCluster, userData *conf.UserData) {
	m := c.Machines()[0]

	checkIfMountpointIsRaid(c, m, "/var/lib/data")

	// reboot it to make sure it comes up again
	err := m.Reboot()
	if err != nil {
		c.Fatalf("could not reboot machine: %v", err)
	}

	checkIfMountpointIsRaid(c, m, "/var/lib/data")
}

func DataOnRaidSecondaryDisk(c cluster.TestCluster, userData *conf.UserData) {
	options := platform.MachineOptions{
		AdditionalDisks: []platform.Disk{
			{Size: "520M", DeviceOpts: []string{"serial=secondary"}},
		},
	}
	m, err := tutil.NewMachineWithOptions(c, userData, options)
	if err != nil {
		c.Fatal(err)
	}

	checkIfMountpointIsRaid(c, m, "/var/lib/data")

	// reboot it to make sure it comes up again
	err = m.Reboot()
	if err != nil {
		c.Fatalf("could not reboot machine: %v", err)
	}

	checkIfMountpointIsRaid(c, m, "/var/lib/data")
}

func checkIfMountpointIsRaid(c cluster.TestCluster, m platform.Machine, mountpoint string) {
	tutil.CheckMountpoint(c, m, mountpoint, func(b tutil.Blockdevice) bool { return isValidRaidType(b.Type) })
}

// isValidRaidType checks if the given type string is one of the possible
// RAID types supported by the testsuite. For example, raid0 or raid1.
func isValidRaidType(rType string) bool {
	if _, ok := raidTypes[rType]; ok {
		return true
	}
	return false
}
