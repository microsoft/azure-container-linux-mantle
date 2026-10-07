// Copyright 2026 Microsoft Corporation.
// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/flatcar/mantle/kola/cluster"
	"github.com/flatcar/mantle/kola/register"
	"github.com/kballard/go-shellquote"
)

const (
	manifestDir = "/usr/share/os-manifests"

	// The image manifest. Sysexts add package-manifest.<sysext>.spdx.json
	// alongside it once they are merged.
	imageManifestName = "package-manifest.spdx.json"
)

type spdxManifest struct {
	Packages []spdxPackage `json:"packages"`
}

type spdxPackage struct {
	SPDXID       string                  `json:"SPDXID"`
	Name         string                  `json:"name"`
	VersionInfo  string                  `json:"versionInfo"`
	ExternalRefs []spdxExternalReference `json:"externalRefs"`
}

type spdxExternalReference struct {
	ReferenceType    string `json:"referenceType"`
	ReferenceLocator string `json:"referenceLocator"`
}

type manifestNevra struct {
	name    string
	epoch   uint64
	version string
	release string
	arch    string
}

func (n manifestNevra) String() string {
	return fmt.Sprintf("%s-%d:%s-%s.%s", n.name, n.epoch, n.version, n.release, n.arch)
}

type manifestPackage struct {
	manifest string
	nevra    manifestNevra
}

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
func packageManifestTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	if err := checkRemotePackageManifests(func(cmd string) ([]byte, error) {
		return c.SSH(m, cmd)
	}); err != nil {
		c.Fatalf("%v", err)
	}
}

func checkRemotePackageManifests(ssh func(string) ([]byte, error)) error {
	var errs []error
	// Don't pipe here: a pipeline reports its last command's status, which
	// could hide find's non-zero exit on a missing directory.
	findCmd := fmt.Sprintf("find %s -maxdepth 1 -type f -name '*.spdx.json'", manifestDir)
	out, err := ssh(findCmd)
	if err != nil {
		errs = append(errs, fmt.Errorf("finding package manifests in %s: %w", manifestDir, err))
	}

	manifestPaths := strings.Split(string(out), "\n")
	sort.Strings(manifestPaths)
	contentsByManifestName := make(map[string][]byte)
	for _, manifestPath := range manifestPaths {
		if manifestPath == "" {
			continue
		}
		contents, err := ssh(shellquote.Join("cat", "--", manifestPath))
		if err != nil {
			errs = append(errs, fmt.Errorf("reading %s: %w", manifestPath, err))
			continue
		}
		contentsByManifestName[path.Base(manifestPath)] = contents
	}
	errs = append(errs, checkDisjointPackageManifests(contentsByManifestName))
	return errors.Join(errs...)
}

func checkDisjointPackageManifests(contentsByManifest map[string][]byte) error {
	var errs []error
	if _, exists := contentsByManifest[imageManifestName]; !exists {
		errs = append(errs, fmt.Errorf("%s missing from %s", imageManifestName, manifestDir))
	}

	manifestNames := make([]string, 0, len(contentsByManifest))
	for name := range contentsByManifest {
		manifestNames = append(manifestNames, name)
	}
	sort.Strings(manifestNames)

	seenPackagesByName := make(map[string][]manifestPackage)
	for _, manifestName := range manifestNames {
		isSysextManifest, _ := path.Match("package-manifest.*.spdx.json", manifestName)
		if manifestName != imageManifestName && !isSysextManifest {
			errs = append(errs, fmt.Errorf("unexpected SPDX manifest %s", manifestName))
			// Still validate contents so an unexpected filename cannot hide
			// conflicting package identities. If it isn't a package manifest,
			// it will likely fail the next step (JSON unmarshalling).
		}

		var manifest spdxManifest

		if err := json.Unmarshal(contentsByManifest[manifestName], &manifest); err != nil {
			errs = append(errs, fmt.Errorf("parsing %s: %w", manifestName, err))
			continue
		}

		if manifest.Packages == nil {
			errs = append(errs, fmt.Errorf("%s has no packages array", manifestName))
			continue
		}

		packageCount := 0
		seenPackageNames := make(map[string]bool)
		for i, pkg := range manifest.Packages {
			if pkg.SPDXID == "SPDXRef-DocumentRoot" {
				continue
			}
			packageCount++

			if pkg.Name == "" {
				errs = append(errs, fmt.Errorf("%s contains a package without a name at index %d", manifestName, i))
				continue
			}

			if seenPackageNames[pkg.Name] {
				errs = append(errs, fmt.Errorf("package %q appears more than once in %s", pkg.Name, manifestName))
				continue
			}
			seenPackageNames[pkg.Name] = true

			nevra, err := packageNEVRA(pkg)
			if err != nil {
				errs = append(errs, fmt.Errorf("package %q at index %d in %s has invalid NEVRA: %w",
					pkg.Name, i, manifestName, err))
				continue
			}

			for _, seenPackage := range seenPackagesByName[pkg.Name] {
				if nevra == seenPackage.nevra {
					continue
				}
				errs = append(errs, fmt.Errorf("package sets are not disjoint: package %q appears in both %s (%s) and %s (%s)",
					pkg.Name, seenPackage.manifest, seenPackage.nevra, manifestName, nevra))
			}
			seenPackagesByName[pkg.Name] = append(seenPackagesByName[pkg.Name], manifestPackage{
				manifest: manifestName,
				nevra:    nevra,
			})
		}
		if packageCount == 0 {
			errs = append(errs, fmt.Errorf("%s has no packages", manifestName))
		}
	}
	return errors.Join(errs...)
}

func packageNEVRA(pkg spdxPackage) (manifestNevra, error) {
	if pkg.Name == "" {
		return manifestNevra{}, fmt.Errorf("missing package name")
	}

	if pkg.VersionInfo == "" {
		return manifestNevra{}, fmt.Errorf("missing versionInfo")
	}

	nevra := manifestNevra{name: pkg.Name}
	versionRelease := pkg.VersionInfo
	epochText, remaining, hasVersionEpoch := strings.Cut(versionRelease, ":")
	if hasVersionEpoch {
		epoch, err := strconv.ParseUint(epochText, 10, 64)
		if err != nil {
			return manifestNevra{}, fmt.Errorf("invalid epoch in versionInfo %q: %w", pkg.VersionInfo, err)
		}
		nevra.epoch = epoch
		versionRelease = remaining
	}

	separator := strings.LastIndexByte(versionRelease, '-')
	if separator <= 0 || separator == len(versionRelease)-1 {
		return manifestNevra{}, fmt.Errorf("versionInfo %q must contain version-release", pkg.VersionInfo)
	}

	nevra.version = versionRelease[:separator]
	nevra.release = versionRelease[separator+1:]

	found := false
	for _, reference := range pkg.ExternalRefs {
		if reference.ReferenceType != "purl" || !strings.HasPrefix(reference.ReferenceLocator, "pkg:rpm/") {
			continue
		}

		if found {
			return manifestNevra{}, fmt.Errorf("multiple RPM package URLs")
		}

		// Parse e.g. pkg:rpm/azurelinux/bash@5.2-1.azl3?arch=x86_64&epoch=1
		packageURL, err := url.Parse(reference.ReferenceLocator)
		if err != nil {
			return manifestNevra{}, fmt.Errorf("invalid RPM package URL: %w", err)
		}

		// Split e.g. rpm/azurelinux/bash@5.2-1.azl3 on @
		encodedNamePath, encodedUrlVersion, hasVr := strings.Cut(packageURL.Opaque, "@")
		if !hasVr {
			return manifestNevra{}, fmt.Errorf("RPM package URL has no version")
		}

		// Get e.g. bash from rpm/azurelinux/bash
		name, err := url.PathUnescape(path.Base(encodedNamePath))
		if err != nil {
			return manifestNevra{}, fmt.Errorf("invalid name in RPM package URL: %w", err)
		}

		if name != pkg.Name {
			return manifestNevra{}, fmt.Errorf("RPM package URL name %q does not match %q", name, pkg.Name)
		}

		urlVersion, err := url.PathUnescape(encodedUrlVersion)
		if err != nil {
			return manifestNevra{}, fmt.Errorf("invalid version in RPM package URL: %w", err)
		}

		if urlVersion != versionRelease {
			return manifestNevra{}, fmt.Errorf("RPM package URL version %q does not match version-release %q",
				urlVersion, versionRelease)
		}

		qualifiers, err := url.ParseQuery(packageURL.RawQuery)
		if err != nil {
			return manifestNevra{}, fmt.Errorf("invalid RPM package URL qualifiers: %w", err)
		}

		if len(qualifiers["arch"]) != 1 || qualifiers.Get("arch") == "" {
			return manifestNevra{}, fmt.Errorf("RPM package URL must contain exactly one nonempty arch qualifier")
		}

		epochValues, hasURLEpoch := qualifiers["epoch"]
		if hasURLEpoch != hasVersionEpoch {
			return manifestNevra{}, fmt.Errorf("epoch must be present in both versionInfo and RPM package URL, or absent from both")
		}

		epoch := uint64(0)
		if hasURLEpoch {
			if len(epochValues) != 1 {
				return manifestNevra{}, fmt.Errorf("RPM package URL must contain exactly one epoch qualifier")
			}

			epoch, err = strconv.ParseUint(epochValues[0], 10, 64)
			if err != nil {
				return manifestNevra{}, fmt.Errorf("invalid epoch in RPM package URL: %w", err)
			}
		}

		if epoch != nevra.epoch {
			return manifestNevra{}, fmt.Errorf("RPM package URL epoch %d does not match versionInfo epoch %d", epoch, nevra.epoch)
		}

		nevra.arch = qualifiers.Get("arch")
		found = true
	}
	if !found {
		return manifestNevra{}, fmt.Errorf("missing RPM package URL")
	}

	return nevra, nil
}
