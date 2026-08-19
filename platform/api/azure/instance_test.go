// Copyright 2026 Microsoft Corporation
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

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v5"
	"github.com/flatcar/mantle/platform"
	"github.com/flatcar/mantle/platform/conf"
)

func TestValidateTrustedLaunchOptions(t *testing.T) {
	const managedImageID = "/subscriptions/test/resourceGroups/test/providers/Microsoft.Compute/images/test"
	const galleryImageID = "/subscriptions/test/resourceGroups/test/providers/Microsoft.Compute/galleries/test/images/test/versions/1.0.0"

	tests := []struct {
		name              string
		options           Options
		expectedErrorText string
	}{
		{
			name: "rejects Hyper-V generation V1",
			options: Options{
				HyperVGeneration: "V1",
				TrustedLaunch:    true,
			},
			expectedErrorText: "Azure Trusted Launch requires Hyper-V generation V2",
		},
		{
			name: "rejects local VHD through managed image",
			options: Options{
				HyperVGeneration: "V2",
				ImageFile:        "test.vhd",
				TrustedLaunch:    true,
			},
			expectedErrorText: "Azure Trusted Launch with --azure-image-file or --azure-blob-url requires --azure-use-gallery",
		},
		{
			name: "rejects blob VHD through managed image",
			options: Options{
				BlobURL:          "https://example.test/test.vhd",
				HyperVGeneration: "V2",
				TrustedLaunch:    true,
			},
			expectedErrorText: "Azure Trusted Launch with --azure-image-file or --azure-blob-url requires --azure-use-gallery",
		},
		{
			name: "rejects legacy managed image ID",
			options: Options{
				DiskURI:          managedImageID,
				HyperVGeneration: "V2",
				TrustedLaunch:    true,
			},
			expectedErrorText: "Azure Trusted Launch requires an Azure Compute Gallery image, not a legacy managed image",
		},
		{
			name: "allows local VHD through gallery",
			options: Options{
				HyperVGeneration: "V2",
				ImageFile:        "test.vhd",
				TrustedLaunch:    true,
				UseGallery:       true,
			},
		},
		{
			name: "allows existing gallery image",
			options: Options{
				DiskURI:          galleryImageID,
				HyperVGeneration: "V2",
				TrustedLaunch:    true,
			},
		},
		{
			name: "preserves standard managed image path",
			options: Options{
				DiskURI:          managedImageID,
				HyperVGeneration: "V2",
			},
		},
		{
			name: "rejects Secure Boot certificate without Trusted Launch",
			options: Options{
				SecureBootCertificateFiles: []string{"test.pem"},
			},
			expectedErrorText: "--azure-secureboot-certificate requires --azure-trusted-launch",
		},
		{
			name: "rejects Secure Boot certificate without Secure Boot",
			options: Options{
				HyperVGeneration:           "V2",
				ImageFile:                  "test.vhd",
				SecureBootCertificateFiles: []string{"test.pem"},
				TrustedLaunch:              true,
				UseGallery:                 true,
			},
			expectedErrorText: "--azure-secureboot-certificate requires --enable-secureboot",
		},
		{
			name: "rejects Secure Boot certificate for existing gallery image",
			options: Options{
				Options: &platform.Options{
					EnableSecureboot: true,
				},
				DiskURI:                    galleryImageID,
				HyperVGeneration:           "V2",
				SecureBootCertificateFiles: []string{"test.pem"},
				TrustedLaunch:              true,
			},
			expectedErrorText: "--azure-secureboot-certificate requires --azure-use-gallery with --azure-image-file or --azure-blob-url",
		},
		{
			name: "allows Secure Boot certificate for gallery VHD",
			options: Options{
				Options: &platform.Options{
					EnableSecureboot: true,
				},
				HyperVGeneration:           "V2",
				ImageFile:                  "test.vhd",
				SecureBootCertificateFiles: []string{"test.pem"},
				TrustedLaunch:              true,
				UseGallery:                 true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateTrustedLaunchOptions(&test.options)
			if test.expectedErrorText == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q", test.expectedErrorText)
			}
			if err.Error() != test.expectedErrorText {
				t.Fatalf("unexpected error: got %q, want %q", err, test.expectedErrorText)
			}
		})
	}
}

func TestTrustedLaunchSecurityProfile(t *testing.T) {
	tests := []struct {
		name              string
		platformOptions   *platform.Options
		size              string
		secureBootEnabled bool
	}{
		{
			name: "amd64",
			platformOptions: &platform.Options{
				Board:            "amd64-usr",
				EnableSecureboot: true,
			},
			size:              "Standard_D2s_v5",
			secureBootEnabled: true,
		},
		{
			name: "arm64",
			platformOptions: &platform.Options{
				Board:            "arm64-usr",
				EnableSecureboot: true,
			},
			size:              "Standard_D2ps_v6",
			secureBootEnabled: true,
		},
		{
			name: "without platform options",
			size: "Standard_D2s_v5",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &API{
				Opts: &Options{
					Options:          test.platformOptions,
					DiskURI:          "test-image",
					HyperVGeneration: "V2",
					Location:         "test-location",
					Size:             test.size,
					TrustedLaunch:    true,
				},
			}
			nicID := "test-nic"
			vm := api.getVMParameters(
				"test-vm",
				"test-key",
				&conf.Conf{},
				nil,
				&armnetwork.Interface{ID: &nicID},
				"",
				InstanceOptions{},
			)

			profile := vm.Properties.SecurityProfile
			if profile == nil {
				t.Fatal("Trusted Launch security profile is missing")
			}
			if profile.SecurityType == nil || *profile.SecurityType != armcompute.SecurityTypesTrustedLaunch {
				t.Fatalf("unexpected security type: %v", profile.SecurityType)
			}
			if profile.UefiSettings == nil {
				t.Fatal("UEFI settings are missing")
			}
			if profile.UefiSettings.SecureBootEnabled == nil || *profile.UefiSettings.SecureBootEnabled != test.secureBootEnabled {
				t.Fatalf("unexpected Secure Boot setting: got %v, want %v", profile.UefiSettings.SecureBootEnabled, test.secureBootEnabled)
			}
			if profile.UefiSettings.VTpmEnabled == nil || !*profile.UefiSettings.VTpmEnabled {
				t.Fatal("vTPM is not enabled")
			}
		})
	}
}
