// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/google/uuid"
	"github.com/tailscale/hujson"

	log "github.com/sirupsen/logrus"
)

const dockerSockForwardContainer = "daytona-sock-forward"

func (d *DockerClient) createProjectFromDevcontainer(project *workspace.Project, projectDir string, logWriter io.Writer) error {
	socketForwardId, err := d.ensureDockerSockForward(logWriter)
	if err != nil {
		return err
	}

	ctx := context.Background()

	configFilePath := ""
	if project.Build != nil && project.Build.Devcontainer != nil && project.Build.Devcontainer.DevContainerFilePath != "" {
		configFilePath = fmt.Sprintf("--config=%s ", filepath.Join(projectDir, project.Build.Devcontainer.DevContainerFilePath))
	}

	devcontainerConfigContent, err := os.ReadFile(filepath.Join(projectDir, project.Build.Devcontainer.DevContainerFilePath))
	if err != nil {
		return err
	}

	var devcontainerConfig map[string]interface{}

	standardized, err := hujson.Standardize(devcontainerConfigContent)
	if err != nil {
		return err
	}

	err = json.Unmarshal(standardized, &devcontainerConfig)
	if err != nil {
		return err
	}

	envVars := map[string]string{}

	if _, ok := devcontainerConfig["containerEnv"]; ok {
		if containerEnv, ok := devcontainerConfig["containerEnv"].(map[string]interface{}); ok {
			for k, v := range containerEnv {
				envVars[k] = v.(string)
			}
		}
	}

	for k, v := range project.EnvVars {
		envVars[k] = v
	}

	workspaceFolder := "/" + project.Name

	envVars["DAYTONA_PROJECT_DIR"] = workspaceFolder

	devcontainerConfig["containerEnv"] = envVars
	devcontainerConfig["workspaceMount"] = fmt.Sprintf("source=${localWorkspaceFolder},target=%s,type=bind", workspaceFolder)
	devcontainerConfig["workspaceFolder"] = workspaceFolder

	configString, err := json.MarshalIndent(devcontainerConfig, "", "  ")
	if err != nil {
		return err
	}

	cmd := []string{"-c", fmt.Sprintf("echo '%s' > /tmp/daytona-devcontainer.json && devcontainer up --workspace-folder=%s %s--override-config=/tmp/daytona-devcontainer.json --id-label=daytona.workspace.id=%s --id-label=daytona.project.name=%s --remove-existing-container", configString, projectDir, configFilePath, project.WorkspaceId, project.Name)}

	c, err := d.apiClient.ContainerCreate(ctx, &container.Config{
		Image:        "daytonaio/workspace-project",
		Entrypoint:   []string{"sh"},
		Env:          []string{"DOCKER_HOST=tcp://localhost:2375"},
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}, &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(fmt.Sprintf("container:%s", socketForwardId)),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: projectDir,
				Target: projectDir,
			},
		},
	}, nil, nil, uuid.NewString())
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

func (d *DockerClient) ensureDockerSockForward(logWriter io.Writer) (string, error) {
	ctx := context.Background()

	containers, err := d.apiClient.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, container := range containers {
		if container.Names[0] == "/"+dockerSockForwardContainer {
			return container.ID, nil
		}
	}

	// TODO: This image should be configurable because it might be hosted on an alternative registry
	err = d.PullImage("alpine/socat", nil, logWriter)
	if err != nil {
		return "", err
	}

	c, err := d.apiClient.ContainerCreate(ctx, &container.Config{
		Image: "alpine/socat",
		User:  "root",
		Cmd:   []string{"tcp-listen:2375,fork,reuseaddr", "unix-connect:/var/run/docker.sock"},
	}, &container.HostConfig{
		Privileged: true,
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: "/var/run/docker.sock",
				Target: "/var/run/docker.sock",
			},
		},
	}, nil, nil, dockerSockForwardContainer)
	if err != nil {
		return "", err
	}

	return c.ID, d.apiClient.ContainerStart(ctx, dockerSockForwardContainer, container.StartOptions{})
}
