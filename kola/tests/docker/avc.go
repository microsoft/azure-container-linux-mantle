// Copyright (c) Microsoft Corporation.
// Licensed under the Apache License, Version 2.0.

package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

func selinuxAVCMessages(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	denial := regexp.MustCompile(`(?i)avc:\s*denied.*`)
	kernelEvent := regexp.MustCompile(`audit: type=1400 audit\(([0-9]+)\.([0-9]{3}):([0-9]+)\):`)
	seen := make(map[[4]string]bool)
	var messages []string
	for {
		var entry struct {
			Message   string `json:"MESSAGE"`
			BootID    string `json:"_BOOT_ID"`
			Transport string `json:"_TRANSPORT"`
			AuditType string `json:"_AUDIT_TYPE"`
			AuditID   string `json:"_AUDIT_ID"`
			Timestamp string `json:"_SOURCE_REALTIME_TIMESTAMP"`
		}
		if err := decoder.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode journal entry: %w", err)
		}
		message := denial.FindString(entry.Message)
		if message == "" {
			return nil, fmt.Errorf("journal entry has no AVC denial: %q", entry.Message)
		}
		id, timestamp := "", ""
		switch entry.Transport {
		case "audit":
			if entry.AuditType == "1400" {
				id, timestamp = entry.AuditID, entry.Timestamp
			}
		case "kernel":
			if match := kernelEvent.FindStringSubmatch(entry.Message); match != nil {
				id, timestamp = match[3], match[1]+match[2]+"000"
			}
		}
		// Journald can receive one event through both audit and kernel transports.
		// Preserve distinct events, differing payloads, and records lacking identity.
		if entry.BootID != "" && id != "" && timestamp != "" {
			key := [4]string{entry.BootID, timestamp, id, message}
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		messages = append(messages, message)
	}
	return messages, nil
}
