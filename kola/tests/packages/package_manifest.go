// Copyright 2026 Microsoft Corporation.
// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"fmt"
	"path"
	"strings"

	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
)

const (
	manifestDir = "/usr/share/os-manifests"

	// The image manifest. Sysexts add package-manifest.<sysext>.spdx.json
	// alongside it once they are merged.
	imageManifestName = "package-manifest.spdx.json"
)

func init() {
	register.Register(&register.Test{
		Run:         packageManifestTest,
		ClusterSize: 1,
		Name:        "acl.packages.package-manifest",
		// Written by write_package_manifest in azure-container-linux, so
		// upstream Container Linux images do not carry these files.
		Distros:   []string{"acl"},
		Platforms: []string{"qemu", "qemu-unpriv", "azure"},
	})
}

// The documents' contents are checked against a golden SPDX 2.2 document by the
// Build RPMs job of the ACL GitHub PR pipeline, which runs
// build_library/rpm/tests/test_generate_package_manifest.sh from
// azure-container-linux.
// What only a booted image can show is that the files were written into the
// rootfs at all, so that is all this checks.
func packageManifestTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// -size +0c because an empty file is a generator that ran and produced
	// nothing, which is a failure this test would otherwise pass.
	// Don't pipe here: a pipeline reports its last command's status, which
	// would hide find's non-zero exit on a missing directory from MustSSH.
	findCmd := fmt.Sprintf("find %s -maxdepth 1 -type f -size +0c -name '*.spdx.json'", manifestDir)
	out := c.MustSSH(m, findCmd)

	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			names = append(names, path.Base(line))
		}
	}
	if len(names) == 0 {
		c.Fatalf("no *.spdx.json files in %s", manifestDir)
	}

	for _, name := range names {
		if name == imageManifestName {
			return
		}
	}

	c.Errorf("%s missing from %s, found %v", imageManifestName, manifestDir, names)
}
