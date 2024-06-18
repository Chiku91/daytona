// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"io"

	"github.com/daytonaio/daytona/pkg/containerregistry"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type IDockerClient interface {
	CreateProject(project *workspace.Project, projectDir string, cr *containerregistry.ContainerRegistry, logWriter io.Writer) error
	CreateWorkspace(workspace *workspace.Workspace, logWriter io.Writer) error

	DestroyProject(project *workspace.Project) error
	DestroyWorkspace(workspace *workspace.Workspace) error

	StartProject(project *workspace.Project, daytonaDownloadUrl string, logWriter io.Writer) error
	StopProject(project *workspace.Project, logWriter io.Writer) error

	GetProjectInfo(project *workspace.Project) (*workspace.ProjectInfo, error)
	GetWorkspaceInfo(ws *workspace.Workspace) (*workspace.WorkspaceInfo, error)

	GetProjectContainerName(project *workspace.Project) string
	GetProjectVolumeName(project *workspace.Project) string
	ExecSync(containerID string, config types.ExecConfig, outputWriter io.Writer) (*ExecResult, error)
	GetContainerLogs(containerName string, logWriter io.Writer) error
	PullImage(imageName string, cr *containerregistry.ContainerRegistry, logWriter io.Writer) error
	PushImage(imageName string, cr *containerregistry.ContainerRegistry, logWriter io.Writer) error
}

type DockerClientConfig struct {
	ApiClient client.APIClient
}

func NewDockerClient(config DockerClientConfig) IDockerClient {
	return &DockerClient{
		apiClient: config.ApiClient,
	}
}

type DockerClient struct {
	apiClient client.APIClient
}

func (d *DockerClient) GetProjectContainerName(project *workspace.Project) string {
	containers, err := d.apiClient.ContainerList(context.Background(), container.ListOptions{})
	if err != nil {
		return project.WorkspaceId + "-" + project.Name
	}

	for _, c := range containers {
		if workspaceId, ok := c.Labels["daytona.workspace.id"]; ok && workspaceId == project.WorkspaceId {
			if projectName, ok := c.Labels["daytona.project.name"]; ok && projectName == project.Name {
				return c.ID
			}
		}
	}

	return project.WorkspaceId + "-" + project.Name
}

func (d *DockerClient) GetProjectVolumeName(project *workspace.Project) string {
	return project.WorkspaceId + "-" + project.Name
}
