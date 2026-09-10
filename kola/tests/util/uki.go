// Copyright (c) Microsoft.
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

package util

import (
	"strings"

	"github.com/flatcar/mantle/platform"
)

// IsUki detects whether the current boot used a UKI (systemd-stub) rather
// than GRUB. It probes systemd-stub's volatile StubInfo EFI variable
// (Boot Loader Interface), which is only set when a UKI's stub launched the
// current boot. This reflects the actual boot path, unlike checking for
// installed UKI files under /boot/EFI/Linux, which can be populated on a
// GRUB boot too (e.g. while UKIs are staged/installed) and would misclassify
// the boot mode. Returns an error if the probe itself fails (e.g. SSH/
// machine failure); callers must not treat a probe error as a GRUB boot.
func IsUki(m platform.Machine) (bool, error) {
	out, _, err := m.SSH("ls /sys/firmware/efi/efivars/StubInfo-* >/dev/null 2>&1 && echo uki || echo grub")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "uki", nil
}
