// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types/container"
)

func (d *DockerClient) startImageProject(project *workspace.Project, daytonaDownloadUrl string, logWriter io.Writer) error {
	containerName := d.GetProjectContainerName(project)
	ctx := context.Background()

	c, err := d.apiClient.ContainerInspect(ctx, containerName)
	if err != nil {
		return err
	}

	// TODO: Add logging
	_, composeContainers, err := d.getComposeContainers(c)
	if err != nil {
		return err
	}

	if composeContainers == nil {
		return nil
	}

	if logWriter != nil {
		logWriter.Write([]byte("Stopping compose containers\n"))
	}

	for _, c := range composeContainers {
		err = d.apiClient.ContainerStart(ctx, c.ID, container.StartOptions{})
		if err != nil {
			return err
		}
		if logWriter != nil {
			logWriter.Write([]byte(fmt.Sprintf("Started %s\n", strings.TrimPrefix(c.Names[0], "/"))))
		}
	}

	if err == nil && c.State.Running {
		return d.startDaytonaAgent(project, daytonaDownloadUrl, logWriter)
	}

	err = d.apiClient.ContainerStart(ctx, containerName, container.StartOptions{})
	if err != nil {
		return err
	}

	// make sure container is running
	//	TODO: timeout
	for {
		c, err = d.apiClient.ContainerInspect(ctx, containerName)
		if err != nil {
			return err
		}

		if c.State.Running {
			break
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}
