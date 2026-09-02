// Copyright 2021 Kinvolk GmbH
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
// local package import and it's behavior as a package

package kubeadm

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate(t *testing.T) {
	t.Run("SuccessWithBase64", func(t *testing.T) {
		res, err := render(
			"Hello, {{ .World }}",
			map[string]interface{}{
				"World": "world !",
			},
			true,
		)
		require.Nil(t, err)
		assert.Equal(t, "SGVsbG8sIHdvcmxkICE=", res.String())

	})
	t.Run("Success", func(t *testing.T) {
		res, err := render(
			"Hello, {{ .World }}",
			map[string]interface{}{
				"World": "world !",
			},
			false,
		)
		require.Nil(t, err)
		assert.Equal(t, "Hello, world !", res.String())

	})
	t.Run("SuccessMasterScript", func(t *testing.T) {
		for _, CNI := range CNIs {
			res, err := render(masterScript, GetTestMasterScriptRenderParams(CNI), false)
			require.Nil(t, err)
			script, err := ioutil.ReadFile(fmt.Sprintf("testdata/master-%s-script.sh", CNI))
			require.Nil(t, err)
			assert.Equal(t, string(script), res.String())
		}
	})
	t.Run("SuccessACLFlannelMasterScript", func(t *testing.T) {
		params := GetTestMasterScriptRenderParams("flannel")
		require.NoError(t, configureACLFlannelImages(params, "acl", "amd64"))

		res, err := render(masterScript, params, false)
		require.NoError(t, err)
		assert.Contains(t, res.String(), "https://raw.githubusercontent.com/flannel-io/flannel/v0.25.4/Documentation/kube-flannel.yml")
		assert.Contains(t, res.String(), fmt.Sprintf("replace_flannel_image '%s' '%s' 2", aclFlannelSourceImage, aclFlannelImage))
		assert.Contains(t, res.String(), fmt.Sprintf("replace_flannel_image '%s' '%s' 1", aclFlannelCNISourceImage, aclFlannelCNIImage))
		assert.Contains(t, res.String(), "Unexpected Quay image in kube-flannel.yml")
	})
	t.Run("SuccessMasterConfig", func(t *testing.T) {
		for _, arch := range TestArchitectures {
			res, err := render(masterConfig, GetTestMasterConfigRenderParams(arch), false)
			require.Nil(t, err)
			script, err := ioutil.ReadFile(fmt.Sprintf("testdata/master-cilium-%s-config.yml", arch))
			require.Nil(t, err)
			assert.Equal(t, string(script), res.String())
		}
	})
}

func TestConfigureACLFlannelImages(t *testing.T) {
	t.Run("ACL", func(t *testing.T) {
		params := GetTestMasterScriptRenderParams("flannel")
		require.NoError(t, configureACLFlannelImages(params, "acl", "arm64"))

		assert.Equal(t, aclFlannelVersion, params["FlannelVersion"])
		assert.Equal(t, aclFlannelSourceImage, params["FlannelSourceImage"])
		assert.Equal(t, aclFlannelCNISourceImage, params["FlannelCNISourceImage"])
		assert.Equal(t, aclFlannelImage, params["FlannelImage"])
		assert.Equal(t, aclFlannelCNIImage, params["FlannelCNIImage"])
		assert.Equal(t, aclFlannelImageIndex, params["FlannelImageIndex"])
		assert.Equal(t, aclFlannelCNIImageIndex, params["FlannelCNIImageIndex"])
	})

	t.Run("Flatcar", func(t *testing.T) {
		params := GetTestMasterScriptRenderParams("flannel")
		originalVersion := params["FlannelVersion"]
		require.NoError(t, configureACLFlannelImages(params, "cl", "amd64"))

		assert.Equal(t, originalVersion, params["FlannelVersion"])
		_, configured := params["FlannelImage"]
		assert.False(t, configured)
	})

	t.Run("OtherCNI", func(t *testing.T) {
		params := GetTestMasterScriptRenderParams("cilium")
		require.NoError(t, configureACLFlannelImages(params, "acl", "amd64"))

		_, configured := params["FlannelImage"]
		assert.False(t, configured)
	})

	t.Run("UnsupportedArchitecture", func(t *testing.T) {
		params := GetTestMasterScriptRenderParams("flannel")
		err := configureACLFlannelImages(params, "acl", "s390x")
		assert.EqualError(t, err, `unsupported architecture "s390x" for ACL Flannel images`)
	})
}
