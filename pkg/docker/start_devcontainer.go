// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"io"

	"github.com/daytonaio/daytona/pkg/workspace"
)

func (d *DockerClient) startDevcontainerProject(project *workspace.Project, projectDir string, logWriter io.Writer) error {
	err := d.createProjectFromDevcontainer(project, projectDir, logWriter, false)
	if err != nil {
		return err
	}

	return nil
}
