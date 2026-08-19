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

package main

import "testing"

func TestValidateSecureBootOptions(t *testing.T) {
	tests := []struct {
		name              string
		platform          string
		enableSecureboot  bool
		trustedLaunch     bool
		ovmfVars          string
		expectedErrorText string
	}{
		{
			name:              "Azure rejects Secure Boot without Trusted Launch",
			platform:          "azure",
			enableSecureboot:  true,
			expectedErrorText: "--enable-secureboot on Azure requires --azure-trusted-launch",
		},
		{
			name:             "Azure allows Secure Boot with Trusted Launch",
			platform:         "azure",
			enableSecureboot: true,
			trustedLaunch:    true,
		},
		{
			name:          "Azure allows Trusted Launch without Secure Boot",
			platform:      "azure",
			trustedLaunch: true,
		},
		{
			name:              "QEMU still requires OVMF vars",
			platform:          "qemu",
			enableSecureboot:  true,
			expectedErrorText: "secureboot requires OVMF vars file",
		},
		{
			name:              "Unprivileged QEMU still requires OVMF vars",
			platform:          "qemu-unpriv",
			enableSecureboot:  true,
			expectedErrorText: "secureboot requires OVMF vars file",
		},
		{
			name:             "QEMU allows Secure Boot with OVMF vars",
			platform:         "qemu",
			enableSecureboot: true,
			ovmfVars:         "OVMF_VARS.fd",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSecureBootOptions(test.platform, test.enableSecureboot, test.trustedLaunch, test.ovmfVars)
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

func TestValidateAzureTrustedLaunchSize(t *testing.T) {
	tests := []struct {
		name              string
		platform          string
		size              string
		trustedLaunch     bool
		sizeExplicit      bool
		expectedErrorText string
	}{
		{
			name:              "Azure rejects inherited default size",
			platform:          "azure",
			size:              "Standard_DS2_v2",
			trustedLaunch:     true,
			expectedErrorText: "--azure-trusted-launch requires explicitly setting --azure-size to a Trusted Launch-compatible VM size",
		},
		{
			name:              "Azure rejects explicit empty size",
			platform:          "azure",
			trustedLaunch:     true,
			sizeExplicit:      true,
			expectedErrorText: "--azure-trusted-launch requires explicitly setting --azure-size to a Trusted Launch-compatible VM size",
		},
		{
			name:          "Azure allows explicit AMD64 size",
			platform:      "azure",
			size:          "Standard_D2s_v5",
			trustedLaunch: true,
			sizeExplicit:  true,
		},
		{
			name:          "Azure allows explicit ARM64 size",
			platform:      "azure",
			size:          "Standard_D2ps_v6",
			trustedLaunch: true,
			sizeExplicit:  true,
		},
		{
			name:          "Azure preserves default without Trusted Launch",
			platform:      "azure",
			size:          "Standard_DS2_v2",
			sizeExplicit:  false,
			trustedLaunch: false,
		},
		{
			name:          "Other platforms ignore Azure size",
			platform:      "qemu",
			trustedLaunch: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAzureTrustedLaunchSize(test.platform, test.size, test.trustedLaunch, test.sizeExplicit)
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
