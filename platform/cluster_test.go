// Copyright (c) Microsoft Corporation.
// Licensed under the Apache License, Version 2.0.

package platform

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/flatcar/mantle/platform/conf"
)

func TestACLCoreSudoersProvisioning(t *testing.T) {
	for _, tc := range []struct {
		name string
		data *conf.UserData
	}{
		{"ignition-v1", conf.Ignition(`{"ignitionVersion":1}`)},
		{"ignition-v2", conf.Ignition(`{"ignition":{"version":"2.0.0"}}`)},
		{"ignition-v3", conf.Ignition(`{"ignition":{"version":"3.0.0"}}`)},
		{"script", conf.Script("#!/bin/bash\n")},
		{"cloud-config", conf.CloudConfig("#cloud-config\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bc := &BaseCluster{
				bf:    &BaseFlight{baseopts: &Options{Distribution: "acl"}},
				rconf: &RuntimeConfig{NoSSHKeyInUserData: true, NoDisableUpdates: true},
			}
			rendered, err := bc.RenderUserData(tc.data, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := rendered.String()
			if !rendered.IsIgnition() {
				if !strings.Contains(output, "/etc/sudoers.d/kola-core-nopasswd") {
					t.Fatal("non-Ignition config is missing the sudoers file")
				}
				return
			}
			if !strings.Contains(output, "kola-core-setup.service") {
				t.Fatal("Ignition is missing the core setup unit")
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(output)); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(compact.String(), `"path":"/etc/sudoers.d/kola-core-nopasswd"`) {
				t.Fatal("Ignition must let the setup unit write sudoers through /etc's overlay")
			}
		})
	}
}
