package packages

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCheckDisjointPackageManifests(test *testing.T) {
	const (
		bashPackage      = `{"name":"bash","versionInfo":"5.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.2-1.azl3?arch=x86_64"}]}`
		newBashPackage   = `{"name":"bash","versionInfo":"5.3-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.3-1.azl3?arch=x86_64"}]}`
		systemdPackage   = `{"name":"systemd","versionInfo":"255-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/systemd@255-1.azl3?arch=x86_64"}]}`
		gdbPackage       = `{"name":"gdb","versionInfo":"15.1-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/gdb@15.1-1.azl3?arch=x86_64"}]}`
		stracePackage    = `{"name":"strace","versionInfo":"6.9-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/strace@6.9-1.azl3?arch=x86_64"}]}`
		newStracePackage = `{"name":"strace","versionInfo":"6.10-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/strace@6.10-1.azl3?arch=x86_64"}]}`
		nvidiaPackage    = `{"name":"nvidia-container-toolkit","versionInfo":"1.16.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/nvidia-container-toolkit@1.16.2-1.azl3?arch=x86_64"}]}`
	)

	testCases := []struct {
		name      string
		manifests map[string]string
		wantError string
	}{
		{
			name: "image without sysexts",
			manifests: map[string]string{
				imageManifestName: `{"packages":[` + bashPackage + `]}`,
			},
		},
		{
			name: "disjoint image and sysexts",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `,` + systemdPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[` + gdbPackage + `]}`,
				"package-manifest.tools.spdx.json": `{"packages":[` + stracePackage + `]}`,
			},
		},
		{
			name: "document root shares real package name",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"azurecontainerlinux"},` + bashPackage + `]}`,
				"package-manifest.nvidia-container-toolkit.spdx.json": `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"nvidia-container-toolkit"},` + nvidiaPackage + `]}`,
			},
		},
		{
			name: "document roots can share a name across manifests",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"azurecontainerlinux"},` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"azurecontainerlinux"},` + gdbPackage + `]}`,
			},
		},
		{
			name: "document roots do not hide real package overlaps",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"bash"},` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"bash"},` + newBashPackage + `]}`,
			},
			wantError: `package sets are not disjoint: package "bash" appears in both package-manifest.debug.spdx.json (bash-0:5.3-1.azl3.x86_64) and package-manifest.spdx.json (bash-0:5.2-1.azl3.x86_64)`,
		},
		{
			name: "image and sysext overlap",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[` + newBashPackage + `]}`,
			},
			wantError: `package sets are not disjoint: package "bash" appears in both package-manifest.debug.spdx.json (bash-0:5.3-1.azl3.x86_64) and package-manifest.spdx.json (bash-0:5.2-1.azl3.x86_64)`,
		},
		{
			name: "two sysexts overlap",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[` + stracePackage + `]}`,
				"package-manifest.tools.spdx.json": `{"packages":[` + newStracePackage + `]}`,
			},
			wantError: `package sets are not disjoint: package "strace" appears in both package-manifest.debug.spdx.json (strace-0:6.9-1.azl3.x86_64) and package-manifest.tools.spdx.json (strace-0:6.10-1.azl3.x86_64)`,
		},
		{
			name: "same NEVRA across manifests",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[{"name":"bash","versionInfo":"1:5.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.2-1.azl3?arch=x86_64&epoch=1"}]}]}`,
				"package-manifest.debug.spdx.json": `{"packages":[{"name":"bash","versionInfo":"1:5.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.2-1.azl3?epoch=1&arch=x86_64"}]}]}`,
				"package-manifest.tools.spdx.json": `{"packages":[{"name":"bash","versionInfo":"1:5.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.2-1.azl3?arch=x86_64&epoch=1"}]}]}`,
			},
		},
		{
			name: "same NEVRA within one manifest",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"name":"bash","versionInfo":"5.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.2-1.azl3?arch=x86_64"}]},{"name":"bash","versionInfo":"5.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.2-1.azl3?arch=x86_64"}]}]}`,
			},
			wantError: `package "bash" appears more than once in package-manifest.spdx.json`,
		},
		{
			name: "different versions still overlap",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[` + newBashPackage + `]}`,
			},
			wantError: `package sets are not disjoint: package "bash" appears in both package-manifest.debug.spdx.json (bash-0:5.3-1.azl3.x86_64) and package-manifest.spdx.json (bash-0:5.2-1.azl3.x86_64)`,
		},
		{
			name: "same name within one manifest",
			manifests: map[string]string{
				imageManifestName: `{"packages":[` + bashPackage + `,` + newBashPackage + `]}`,
			},
			wantError: `package "bash" appears more than once in package-manifest.spdx.json`,
		},
		{
			name: "empty image package set",
			manifests: map[string]string{
				imageManifestName: `{"packages":[]}`,
			},
			wantError: "package-manifest.spdx.json has no packages",
		},
		{
			name: "empty sysext package set",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[]}`,
			},
			wantError: "package-manifest.debug.spdx.json has no packages",
		},
		{
			name: "image contains only document root",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"azurecontainerlinux"}]}`,
			},
			wantError: "package-manifest.spdx.json has no packages",
		},
		{
			name: "sysext contains only document root",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[{"SPDXID":"SPDXRef-DocumentRoot","name":"debug"}]}`,
			},
			wantError: "package-manifest.debug.spdx.json has no packages",
		},
		{
			name:      "no manifests",
			wantError: "package-manifest.spdx.json missing from /usr/share/os-manifests",
		},
		{
			name: "missing image manifest",
			manifests: map[string]string{
				"package-manifest.debug.spdx.json": `{"packages":[` + gdbPackage + `]}`,
			},
			wantError: "package-manifest.spdx.json missing from /usr/share/os-manifests",
		},
		{
			name: "invalid image JSON",
			manifests: map[string]string{
				imageManifestName: `{`,
			},
			wantError: "parsing package-manifest.spdx.json: unexpected end of JSON input",
		},
		{
			name: "empty sysext manifest",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": "",
			},
			wantError: "parsing package-manifest.debug.spdx.json: unexpected end of JSON input",
		},
		{
			name: "packages is an object",
			manifests: map[string]string{
				imageManifestName: `{"packages":{}}`,
			},
			wantError: "parsing package-manifest.spdx.json: json: cannot unmarshal object into Go struct field spdxManifest.packages of type []packages.spdxPackage",
		},
		{
			name: "package name is a number",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"name":123}]}`,
			},
			wantError: "parsing package-manifest.spdx.json: json: cannot unmarshal number into Go struct field spdxPackage.packages.name of type string",
		},
		{
			name: "missing packages array",
			manifests: map[string]string{
				imageManifestName: `{}`,
			},
			wantError: "package-manifest.spdx.json has no packages array",
		},
		{
			name: "null packages array",
			manifests: map[string]string{
				imageManifestName: `{"packages":null}`,
			},
			wantError: "package-manifest.spdx.json has no packages array",
		},
		{
			name: "missing package name",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{}]}`,
			},
			wantError: "package-manifest.spdx.json contains a package without a name at index 0",
		},
		{
			name: "empty package name",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"name":""}]}`,
			},
			wantError: "package-manifest.spdx.json contains a package without a name at index 0",
		},
		{
			name: "unexpected SPDX filename",
			manifests: map[string]string{
				imageManifestName: `{"packages":[` + bashPackage + `]}`,
				"other.spdx.json": `{"packages":[` + gdbPackage + `]}`,
			},
			wantError: "unexpected SPDX manifest other.spdx.json",
		},
		{
			name: "unexpected manifest still checked for overlaps",
			manifests: map[string]string{
				imageManifestName:                 `{"packages":[` + bashPackage + `]}`,
				"package-manifestdebug.spdx.json": `{"packages":[` + newBashPackage + `]}`,
			},
			wantError: strings.Join([]string{
				"unexpected SPDX manifest package-manifestdebug.spdx.json",
				`package sets are not disjoint: package "bash" appears in both package-manifest.spdx.json (bash-0:5.2-1.azl3.x86_64) and package-manifestdebug.spdx.json (bash-0:5.3-1.azl3.x86_64)`,
			}, "\n"),
		},
		{
			name: "all validation errors across manifests",
			manifests: map[string]string{
				imageManifestName:                    `{"packages":[{},` + bashPackage + `,` + bashPackage + `,{}]}`,
				"package-manifest.debug.spdx.json":   `{`,
				"package-manifest.empty.spdx.json":   `{"packages":[]}`,
				"package-manifest.missing.spdx.json": `{}`,
			},
			wantError: strings.Join([]string{
				"parsing package-manifest.debug.spdx.json: unexpected end of JSON input",
				"package-manifest.empty.spdx.json has no packages",
				"package-manifest.missing.spdx.json has no packages array",
				"package-manifest.spdx.json contains a package without a name at index 0",
				`package "bash" appears more than once in package-manifest.spdx.json`,
				"package-manifest.spdx.json contains a package without a name at index 3",
			}, "\n"),
		},
		{
			name: "unique package missing versionInfo",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"name":"bash"}]}`,
			},
			wantError: `package "bash" at index 0 in package-manifest.spdx.json has invalid NEVRA: missing versionInfo`,
		},
		{
			name: "unique package missing RPM URL",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"name":"bash","versionInfo":"5.2-1.azl3"}]}`,
			},
			wantError: `package "bash" at index 0 in package-manifest.spdx.json has invalid NEVRA: missing RPM package URL`,
		},
		{
			name: "unique sysext package missing architecture",
			manifests: map[string]string{
				imageManifestName:                  `{"packages":[` + bashPackage + `]}`,
				"package-manifest.debug.spdx.json": `{"packages":[{"name":"gdb","versionInfo":"15.1-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/gdb@15.1-1.azl3"}]}]}`,
			},
			wantError: `package "gdb" at index 0 in package-manifest.debug.spdx.json has invalid NEVRA: RPM package URL must contain exactly one nonempty arch qualifier`,
		},
		{
			name: "all invalid NEVRAs are reported",
			manifests: map[string]string{
				imageManifestName: `{"packages":[` + bashPackage + `,{"name":"gdb"},{"name":"strace","versionInfo":"6.9-1.azl3"}]}`,
			},
			wantError: strings.Join([]string{
				`package "gdb" at index 1 in package-manifest.spdx.json has invalid NEVRA: missing versionInfo`,
				`package "strace" at index 2 in package-manifest.spdx.json has invalid NEVRA: missing RPM package URL`,
			}, "\n"),
		},
		{
			name: "null package name",
			manifests: map[string]string{
				imageManifestName: `{"packages":[{"name":null}]}`,
			},
			wantError: "package-manifest.spdx.json contains a package without a name at index 0",
		},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			manifests := make(map[string][]byte)
			for name, contents := range testCase.manifests {
				manifests[name] = []byte(contents)
			}
			err := checkDisjointPackageManifests(manifests)
			if testCase.wantError == "" {
				if err != nil {
					test.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil {
				test.Fatalf("expected error %q, got nil", testCase.wantError)
			} else if err.Error() != testCase.wantError {
				test.Fatalf("expected error %q, got %q", testCase.wantError, err.Error())
			}
		})
	}
}

func TestCheckRemotePackageManifests(test *testing.T) {
	const (
		findCmd       = "find /usr/share/os-manifests -maxdepth 1 -type f -name '*.spdx.json'"
		readImage     = "cat -- /usr/share/os-manifests/package-manifest.spdx.json"
		readDebug     = "cat -- /usr/share/os-manifests/package-manifest.debug.spdx.json"
		readTools     = "cat -- /usr/share/os-manifests/package-manifest.tools.spdx.json"
		imageContents = `{"packages":[{"name":"bash","versionInfo":"5.2-1.azl3","externalRefs":[{"referenceType":"purl","referenceLocator":"pkg:rpm/azurelinux/bash@5.2-1.azl3?arch=x86_64"}]}]}`
	)
	readErr := errors.New("permission denied")
	findErr := errors.New("directory unreadable")
	testCases := []struct {
		name      string
		outputs   map[string]string
		errors    map[string]error
		commands  []string
		wantError string
	}{
		{
			name: "valid image",
			outputs: map[string]string{
				findCmd:   "/usr/share/os-manifests/package-manifest.spdx.json\n",
				readImage: imageContents,
			},
			commands: []string{findCmd, readImage},
		},
		{
			name: "read failures do not hide validation errors",
			outputs: map[string]string{
				findCmd:   "/usr/share/os-manifests/package-manifest.tools.spdx.json\n/usr/share/os-manifests/package-manifest.spdx.json\n/usr/share/os-manifests/package-manifest.debug.spdx.json\n",
				readDebug: "ignored partial output",
				readImage: "{",
				readTools: "",
			},
			errors:   map[string]error{readDebug: readErr, readTools: readErr},
			commands: []string{findCmd, readDebug, readImage, readTools},
			wantError: strings.Join([]string{
				"reading /usr/share/os-manifests/package-manifest.debug.spdx.json: permission denied",
				"reading /usr/share/os-manifests/package-manifest.tools.spdx.json: permission denied",
				"parsing package-manifest.spdx.json: unexpected end of JSON input",
			}, "\n"),
		},
		{
			name: "partial discovery still reads found manifests",
			outputs: map[string]string{
				findCmd:   "/usr/share/os-manifests/package-manifest.spdx.json\n",
				readImage: "{",
			},
			errors:   map[string]error{findCmd: findErr},
			commands: []string{findCmd, readImage},
			wantError: strings.Join([]string{
				"finding package manifests in /usr/share/os-manifests: directory unreadable",
				"parsing package-manifest.spdx.json: unexpected end of JSON input",
			}, "\n"),
		},
		{
			name:     "discovery failure",
			outputs:  map[string]string{findCmd: ""},
			errors:   map[string]error{findCmd: findErr},
			commands: []string{findCmd},
			wantError: strings.Join([]string{
				"finding package manifests in /usr/share/os-manifests: directory unreadable",
				"package-manifest.spdx.json missing from /usr/share/os-manifests",
			}, "\n"),
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			var commands []string
			err := checkRemotePackageManifests(func(cmd string) ([]byte, error) {
				commands = append(commands, cmd)
				output, exists := testCase.outputs[cmd]
				if !exists {
					test.Fatalf("unexpected SSH command %q", cmd)
				}
				return []byte(output), testCase.errors[cmd]
			})
			if !reflect.DeepEqual(commands, testCase.commands) {
				test.Fatalf("expected commands %v, got %v", testCase.commands, commands)
			}
			if testCase.wantError == "" {
				if err != nil {
					test.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil {
				test.Fatalf("expected error %q, got nil", testCase.wantError)
			} else if err.Error() != testCase.wantError {
				test.Fatalf("expected error %q, got %q", testCase.wantError, err.Error())
			}
		})
	}
}
