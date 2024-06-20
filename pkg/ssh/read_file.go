// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"fmt"
)

func ReadFile(sessionConfig *SessionConfig, filePath string) ([]byte, error) {
	session, err := NewSession(sessionConfig)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	output, err := session.CombinedOutput(fmt.Sprintf("cat %s", filePath))
	if err != nil {
		return nil, err
	}

	return output, nil
}
