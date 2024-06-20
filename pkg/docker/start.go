// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"errors"
	"io"
	"time"

	"github.com/daytonaio/daytona/pkg/builder/devcontainer"
	"github.com/daytonaio/daytona/pkg/provider/util"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types"
)

func (d *DockerClient) StartProject(opts *CreateProjectOptions, daytonaDownloadUrl string) error {
	var err error
	if opts.Project.Build != nil && opts.Project.Build.Devcontainer != nil {
		err = d.startDevcontainerProject(opts)
	} else if devcontainerFilePath, pathError := devcontainer.FindDevcontainerConfigFilePath(opts.ProjectDir); pathError == nil {
		opts.Project.Build.Devcontainer = &workspace.ProjectBuildDevcontainer{
			DevContainerFilePath: devcontainerFilePath,
		}

		err = d.startDevcontainerProject(opts)
	} else {
		err = d.startImageProject(opts)
	}

	if err != nil {
		return err
	}

	return d.startDaytonaAgent(opts.Project, daytonaDownloadUrl, opts.LogWriter)
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
