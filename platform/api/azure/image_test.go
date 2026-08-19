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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
)

func TestGallerySecurityType(t *testing.T) {
	tests := []struct {
		name          string
		trustedLaunch bool
		expected      string
	}{
		{
			name:     "standard",
			expected: gallerySecurityTypeStandard,
		},
		{
			name:          "Trusted Launch",
			trustedLaunch: true,
			expected:      gallerySecurityTypeTrustedLaunchSupported,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := gallerySecurityType(test.trustedLaunch); actual != test.expected {
				t.Fatalf("unexpected security type: got %q, want %q", actual, test.expected)
			}
		})
	}
}

func TestGalleryImageTemplateUsesSecurityTypeParameter(t *testing.T) {
	var template struct {
		Parameters map[string]struct {
			DefaultValue  string   `json:"defaultValue"`
			AllowedValues []string `json:"allowedValues"`
		} `json:"parameters"`
		Resources []struct {
			Type       string `json:"type"`
			Properties struct {
				Features []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"features"`
			} `json:"properties"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(galleryImageTemplate, &template); err != nil {
		t.Fatalf("failed to unmarshal gallery image template: %v", err)
	}

	securityType, ok := template.Parameters["securityType"]
	if !ok {
		t.Fatal("gallery image template is missing securityType parameter")
	}
	if securityType.DefaultValue != gallerySecurityTypeStandard {
		t.Fatalf("unexpected default security type: got %q, want %q", securityType.DefaultValue, gallerySecurityTypeStandard)
	}
	allowedSecurityTypes := make(map[string]bool, len(securityType.AllowedValues))
	for _, allowedValue := range securityType.AllowedValues {
		allowedSecurityTypes[allowedValue] = true
	}
	if len(securityType.AllowedValues) != 2 ||
		!allowedSecurityTypes[gallerySecurityTypeStandard] ||
		!allowedSecurityTypes[gallerySecurityTypeTrustedLaunchSupported] {
		t.Fatalf("unexpected allowed security types: %v", securityType.AllowedValues)
	}

	for _, resource := range template.Resources {
		if resource.Type != "Microsoft.Compute/galleries/images" {
			continue
		}
		for _, feature := range resource.Properties.Features {
			if feature.Name == "SecurityType" {
				if feature.Value != "[parameters('securityType')]" {
					t.Fatalf("unexpected SecurityType feature value: %q", feature.Value)
				}
				return
			}
		}
		t.Fatal("gallery image definition is missing SecurityType feature")
	}
	t.Fatal("gallery image template is missing image definition resource")
}

func testCertificate(t *testing.T, commonName string) ([]byte, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create test certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), base64.StdEncoding.EncodeToString(der)
}

func TestLoadSecureBootCertificates(t *testing.T) {
	certPEM, expected := testCertificate(t, "test")
	path := filepath.Join(t.TempDir(), "test.pem")
	if err := os.WriteFile(path, append(certPEM, certPEM...), 0600); err != nil {
		t.Fatalf("failed to write test certificate: %v", err)
	}

	certificates, err := loadSecureBootCertificates([]string{path})
	if err != nil {
		t.Fatalf("failed to load Secure Boot certificate: %v", err)
	}
	if len(certificates) != 1 || certificates[0] != expected {
		t.Fatalf("unexpected certificates: got %v, want [%s]", certificates, expected)
	}
}

func TestLoadSecureBootCertificatesRejectsInvalidPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0600); err != nil {
		t.Fatalf("failed to write invalid certificate: %v", err)
	}

	_, err := loadSecureBootCertificates([]string{path})
	if err == nil {
		t.Fatal("expected invalid certificate to be rejected")
	}
}

func galleryImageVersionProperties(t *testing.T, template map[string]interface{}) map[string]interface{} {
	t.Helper()
	resources, ok := template["resources"].([]interface{})
	if !ok {
		t.Fatal("gallery template resources are missing or malformed")
	}
	for _, rawResource := range resources {
		resource, ok := rawResource.(map[string]interface{})
		if !ok || resource["type"] != galleryImageVersionResourceType {
			continue
		}
		properties, ok := resource["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("gallery image version properties are missing or malformed")
		}
		return properties
	}
	t.Fatal("gallery template is missing image version resource")
	return nil
}

func TestConfigureGallerySecurityProfile(t *testing.T) {
	tests := []struct {
		name          string
		trustedLaunch bool
		certificates  []string
		expectProfile bool
	}{
		{
			name: "standard image",
		},
		{
			name:          "Trusted Launch with Microsoft UEFI template",
			trustedLaunch: true,
			expectProfile: true,
		},
		{
			name:          "Trusted Launch with custom certificate",
			trustedLaunch: true,
			certificates:  []string{"certificate-data"},
			expectProfile: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			template := make(map[string]interface{})
			if err := json.Unmarshal(galleryImageTemplate, &template); err != nil {
				t.Fatalf("failed to unmarshal gallery image template: %v", err)
			}
			if err := configureGallerySecurityProfile(template, test.trustedLaunch, test.certificates); err != nil {
				t.Fatalf("failed to configure gallery security profile: %v", err)
			}

			properties := galleryImageVersionProperties(t, template)
			profile, found := properties["securityProfile"]
			if found != test.expectProfile {
				t.Fatalf("unexpected security profile presence: got %v, want %v", found, test.expectProfile)
			}
			if !test.expectProfile {
				return
			}

			encoded, err := json.Marshal(profile)
			if err != nil {
				t.Fatalf("failed to marshal security profile: %v", err)
			}
			var actual struct {
				UEFISettings struct {
					SignatureTemplateNames []string `json:"signatureTemplateNames"`
					AdditionalSignatures   struct {
						DB []struct {
							Type  string   `json:"type"`
							Value []string `json:"value"`
						} `json:"db"`
					} `json:"additionalSignatures"`
				} `json:"uefiSettings"`
			}
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatalf("failed to unmarshal security profile: %v", err)
			}
			if len(actual.UEFISettings.SignatureTemplateNames) != 1 ||
				actual.UEFISettings.SignatureTemplateNames[0] != string(armcompute.UefiSignatureTemplateNameMicrosoftUefiCertificateAuthorityTemplate) {
				t.Fatalf("unexpected signature templates: %v", actual.UEFISettings.SignatureTemplateNames)
			}
			if len(test.certificates) == 0 {
				if len(actual.UEFISettings.AdditionalSignatures.DB) != 0 {
					t.Fatalf("unexpected additional signatures: %v", actual.UEFISettings.AdditionalSignatures.DB)
				}
				return
			}
			if len(actual.UEFISettings.AdditionalSignatures.DB) != 1 {
				t.Fatalf("unexpected db signatures: %v", actual.UEFISettings.AdditionalSignatures.DB)
			}
			db := actual.UEFISettings.AdditionalSignatures.DB[0]
			if db.Type != string(armcompute.UefiKeyTypeX509) {
				t.Fatalf("unexpected db signature type: got %q", db.Type)
			}
			if len(db.Value) != 1 || db.Value[0] != test.certificates[0] {
				t.Fatalf("unexpected db signature values: %v", db.Value)
			}
		})
	}
}
