// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/daytonaio/daytona/pkg/containerregistry"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

func (d *DockerClient) createProjectFromImage(project *workspace.Project, projectDir string, cr *containerregistry.ContainerRegistry, logWriter io.Writer) error {
	err := d.PullImage(project.Image, cr, logWriter)
	if err != nil {
		return err
	}

	return d.initProjectContainer(project, projectDir)
}

func (d *DockerClient) initProjectContainer(project *workspace.Project, projectDir string) error {
	ctx := context.Background()

	_, err := d.apiClient.ContainerCreate(ctx, GetContainerCreateConfig(project), &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(project.WorkspaceId),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: projectDir,
				Target: fmt.Sprintf("/home/%s/%s", project.User, project.Name),
			},
		},
		ExtraHosts: []string{
			"host.docker.internal:host-gateway",
		},
	}, nil, nil, d.GetProjectContainerName(project))
	if err != nil {
		return err
	}

	return nil
}

func GetContainerCreateConfig(project *workspace.Project) *container.Config {
	envVars := []string{}

	for key, value := range project.EnvVars {
		envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
	}

	return &container.Config{
		Hostname: project.Name,
		Image:    project.Image,
		Labels: map[string]string{
			"daytona.workspace.id":                     project.WorkspaceId,
			"daytona.workspace.project.name":           project.Name,
			"daytona.workspace.project.repository.url": project.Repository.Url,
		},
		User:         project.User,
		Env:          envVars,
		Entrypoint:   []string{"sleep", "infinity"},
		AttachStdout: true,
		AttachStderr: true,
	}
}
