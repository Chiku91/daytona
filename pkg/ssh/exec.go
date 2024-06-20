// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"io"
)

func Exec(sessionConfig *SessionConfig, command string, logWriter io.Writer) error {
	session, err := NewSession(sessionConfig)
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdout = logWriter
	session.Stderr = logWriter

	return session.Run(command)
}
