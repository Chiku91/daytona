// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"io"

	"github.com/daytonaio/daytona/pkg/builder/devcontainer"
	"github.com/daytonaio/daytona/pkg/containerregistry"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types"
)

func (d *DockerClient) CreateWorkspace(workspace *workspace.Workspace, logWriter io.Writer) error {
	if logWriter != nil {
		logWriter.Write([]byte("Initializing network\n"))
	}
	ctx := context.Background()

	networks, err := d.apiClient.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return err
	}

	for _, network := range networks {
		if network.Name == workspace.Id {
			if logWriter != nil {
				logWriter.Write([]byte("Network already exists\n"))
			}
			return nil
		}
	}

	_, err = d.apiClient.NetworkCreate(ctx, workspace.Id, types.NetworkCreate{
		Attachable: true,
		Driver:     "bridge",
	})
	if err != nil {
		return err
	}

	if logWriter != nil {
		logWriter.Write([]byte("Network initialized\n"))
	}
	return nil
}

func (d *DockerClient) CreateProject(project *workspace.Project, projectDir string, cr *containerregistry.ContainerRegistry, logWriter io.Writer) error {
	if project.Build != nil && project.Build.Devcontainer != nil {
		return d.createProjectFromDevcontainer(project, projectDir, logWriter)
	}

	// TODO: rethink the autodetect, make it more reusable
	devcontainerFilePath, err := devcontainer.FindDevcontainerConfigFilePath(projectDir)
	if err == nil {
		project.Build.Devcontainer = &workspace.ProjectBuildDevcontainer{
			DevContainerFilePath: devcontainerFilePath,
		}

		return d.createProjectFromDevcontainer(project, projectDir, logWriter)
	}

	return d.createProjectFromImage(project, projectDir, cr, logWriter)
}
