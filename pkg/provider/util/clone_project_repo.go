// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"io"
	"os/exec"

	"github.com/daytonaio/daytona/pkg/gitprovider"
	"github.com/daytonaio/daytona/pkg/workspace"
)

func GetCloneCommand(project *workspace.Project, daytonaPath, projectDir string, gpc *gitprovider.GitProviderConfig, logWriter io.Writer) ([]string, error) {
	projectJson, err := json.Marshal(project)
	if err != nil {
		return nil, err
	}

	return []string{daytonaPath, "clone", string(projectJson), "--project-dir", projectDir}, nil
}

func CloneProjectRepo(project *workspace.Project, daytonaPath, projectDir string, gpc *gitprovider.GitProviderConfig, logWriter io.Writer) error {
	command, err := GetCloneCommand(project, daytonaPath, projectDir, gpc, logWriter)
	if err != nil {
		return err
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	return cmd.Run()
}
