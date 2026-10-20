// Copyright (c) Microsoft Corporation.
// Licensed under the Apache License, Version 2.0.

package docker

import (
	"fmt"
	"strings"
	"testing"
)

func TestSELinuxAVCMessages(t *testing.T) {
	message := `avc:  denied  { write } for  pid=2591 comm="sh" name="misc" dev="overlay" ino=106 scontext=system_u:system_r:container_t:s0:c197,c739 tcontext=system_u:object_r:etc_t:s0 tclass=dir permissive=0`
	audit := fmt.Sprintf(`{"MESSAGE":%q,"_BOOT_ID":"boot-one","_TRANSPORT":"audit","_AUDIT_TYPE":"1400","_AUDIT_ID":"301","_SOURCE_REALTIME_TIMESTAMP":"1792499378408000"}`, "AVC "+message)
	kernel := fmt.Sprintf(`{"MESSAGE":%q,"_BOOT_ID":"boot-one","_TRANSPORT":"kernel"}`, "audit: type=1400 audit(1792499378.408:301): "+message)
	tests := []struct {
		name    string
		input   string
		count   int
		wantErr bool
	}{
		{"audit only", audit, 1, false},
		{"kernel only", kernel, 1, false},
		{"same event through both transports", audit + "\n" + kernel, 1, false},
		{"kernel arrives first", kernel + "\n" + audit, 1, false},
		{"different event ID", audit + "\n" + strings.Replace(kernel, ":301)", ":302)", 1), 2, false},
		{"different event timestamp", audit + "\n" + strings.Replace(kernel, ".408:", ".409:", 1), 2, false},
		{"different boot", audit + "\n" + strings.Replace(kernel, "boot-one", "boot-two", 1), 2, false},
		{"different denial", audit + "\n" + strings.Replace(kernel, "{ write }", "{ read }", 1), 2, false},
		{"missing event identity", audit + "\n" + strings.Replace(kernel, `"boot-one"`, `""`, 1), 2, false},
		{"missing audit timestamp", strings.Replace(audit, `"1792499378408000"`, `""`, 1) + "\n" + kernel, 2, false},
		{"malformed journal", audit + "\n{", 0, true},
		{"missing denial", `{"MESSAGE":"unrelated"}`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, err := selinuxAVCMessages([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if len(messages) != tt.count {
				t.Fatalf("got %d AVCs, want %d: %v", len(messages), tt.count, messages)
			}
			if tt.count == 1 && messages[0] != message {
				t.Fatalf("denial changed: %q", messages[0])
			}
		})
	}
}
