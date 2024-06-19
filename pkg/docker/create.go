// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/daytonaio/daytona/pkg/builder/devcontainer"
	"github.com/daytonaio/daytona/pkg/containerregistry"
	"github.com/daytonaio/daytona/pkg/gitprovider"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	log "github.com/sirupsen/logrus"
)

func (d *DockerClient) CreateWorkspace(workspace *workspace.Workspace, logWriter io.Writer) error {
	return nil
}

func (d *DockerClient) CreateProject(project *workspace.Project, projectDir string, cr *containerregistry.ContainerRegistry, logWriter io.Writer, gpc *gitprovider.GitProviderConfig) error {
	err := d.cloneProjectRepository(project, projectDir, gpc, logWriter)
	if err != nil {
		return err
	}

	if project.Build != nil && project.Build.Devcontainer != nil {
		err = d.createProjectFromDevcontainer(project, projectDir, logWriter, true)
	} else if devcontainerFilePath, pathError := devcontainer.FindDevcontainerConfigFilePath(projectDir); pathError == nil {
		project.Build.Devcontainer = &workspace.ProjectBuildDevcontainer{
			DevContainerFilePath: devcontainerFilePath,
		}

		err = d.createProjectFromDevcontainer(project, projectDir, logWriter, true)
	} else {
		err = d.createProjectFromImage(project, projectDir, cr, logWriter)
	}

	return err
}

func (d *DockerClient) cloneProjectRepository(project *workspace.Project, projectDir string, gcp *gitprovider.GitProviderConfig, logWriter io.Writer) error {
	// TODO: The image should be configurable
	err := d.PullImage("alpine/git", nil, logWriter)
	if err != nil {
		return err
	}

	ctx := context.Background()

	cloneUrl := project.Repository.Url
	if gcp != nil {
		cloneUrl = fmt.Sprintf("https://%s:%s@%s", gcp.Username, gcp.Token, project.Repository.Url)
	}

	err = os.MkdirAll(filepath.Dir(projectDir), os.ModePerm)
	if err != nil {
		return err
	}

	c, err := d.apiClient.ContainerCreate(ctx, &container.Config{
		Image: "alpine/git",
		Cmd:   []string{"clone", cloneUrl, fmt.Sprintf("/workdir/%s-%s", project.WorkspaceId, project.Name)},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: filepath.Dir(projectDir),
				Target: "/workdir",
			},
		},
	}, nil, nil, d.GetProjectContainerName(project))
	if err != nil {
		return err
	}

	waitResponse, errChan := d.apiClient.ContainerWait(ctx, c.ID, container.WaitConditionNextExit)

	err = d.apiClient.ContainerStart(ctx, c.ID, container.StartOptions{})
	if err != nil {
		return err
	}

	go func() {
		for {
			err = d.GetContainerLogs(c.ID, logWriter)
			if err == nil {
				break
			}
			log.Error(err)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	case resp := <-waitResponse:
		if resp.StatusCode != 0 {
			return fmt.Errorf("container exited with status %d", resp.StatusCode)
		}
		if resp.Error != nil {
			return fmt.Errorf("container exited with error: %s", resp.Error.Message)
		}

		return nil
	}

	return nil
}
