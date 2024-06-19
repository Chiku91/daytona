// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"

	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func (d *DockerClient) DestroyWorkspace(workspace *workspace.Workspace) error {
	ctx := context.Background()

	networks, err := d.apiClient.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return err
	}

	for _, network := range networks {
		if network.Name == workspace.Id {
			err := d.apiClient.NetworkRemove(ctx, network.ID)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (d *DockerClient) DestroyProject(project *workspace.Project) error {
	return d.removeProjectContainer(project)
}

func (d *DockerClient) removeProjectContainer(project *workspace.Project) error {
	ctx := context.Background()

	c, err := d.apiClient.ContainerInspect(ctx, d.GetProjectContainerName(project))
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return err
	}

	err = d.apiClient.ContainerRemove(ctx, d.GetProjectContainerName(project), container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
	if err != nil && !client.IsErrNotFound(err) {
		return err
	}

	err = d.apiClient.VolumeRemove(ctx, d.GetProjectVolumeName(project), true)
	if err != nil && !client.IsErrNotFound(err) {
		return err
	}

	// TODO: Add logging
	projectLabel, composeContainers, err := d.getComposeContainers(c)
	if err != nil {
		return err
	}

	if composeContainers == nil {
		return nil
	}

	for _, c := range composeContainers {
		err = d.apiClient.ContainerRemove(ctx, c.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		})
		if err != nil {
			return err
		}
	}

	err = d.apiClient.NetworkRemove(ctx, fmt.Sprintf("%s_default", projectLabel))
	if err != nil {
		return err
	}

	return nil
}
