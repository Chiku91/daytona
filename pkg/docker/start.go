// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/daytonaio/daytona/pkg/provider/util"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

func (d *DockerClient) StartProject(project *workspace.Project, daytonaDownloadUrl string, logWriter io.Writer) error {
	return d.startProjectContainer(project, daytonaDownloadUrl, logWriter)
}

func (d *DockerClient) startProjectContainer(project *workspace.Project, daytonaDownloadUrl string, logWriter io.Writer) error {
	containerName := d.GetProjectContainerName(project)
	ctx := context.Background()

	inspect, err := d.apiClient.ContainerInspect(ctx, containerName)

	if err == nil && inspect.State.Running {
		return d.startDaytonaAgent(project, daytonaDownloadUrl, logWriter)
	}

	err = d.apiClient.ContainerStart(ctx, containerName, container.StartOptions{})
	if err != nil {
		return err
	}

	// make sure container is running
	//	TODO: timeout
	for {
		inspect, err := d.apiClient.ContainerInspect(ctx, containerName)
		if err != nil {
			return err
		}

		if inspect.State.Running {
			break
		}

		time.Sleep(1 * time.Second)
	}

	return d.startDaytonaAgent(project, daytonaDownloadUrl, logWriter)
}

func (d *DockerClient) startDaytonaAgent(project *workspace.Project, daytonaDownloadUrl string, logWriter io.Writer) error {
	errChan := make(chan error)

	go func() {
		result, err := d.ExecSync(d.GetProjectContainerName(project), types.ExecConfig{
			Cmd:          []string{"bash", "-c", util.GetProjectStartScript(daytonaDownloadUrl, project.ApiKey)},
			AttachStdout: true,
			AttachStderr: true,
		}, logWriter)
		if err != nil {
			errChan <- err
		}

		if result.ExitCode != 0 {
			errChan <- errors.New(result.StdErr)
		}
	}()

	go func() {
		// TODO: Figure out how to check if the agent is running here
		time.Sleep(5 * time.Second)
		errChan <- nil
	}()

	return <-errChan
}
