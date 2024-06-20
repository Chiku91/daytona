// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daytonaio/daytona/pkg/ssh"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/google/uuid"
	"github.com/tailscale/hujson"

	log "github.com/sirupsen/logrus"
)

const dockerSockForwardContainer = "daytona-sock-forward"

func (d *DockerClient) createProjectFromDevcontainer(opts *CreateProjectOptions, prebuild bool) error {
	socketForwardId, err := d.ensureDockerSockForward(opts.LogWriter)
	if err != nil {
		return err
	}

	ctx := context.Background()

	configFilePath := filepath.Join(opts.ProjectDir, opts.Project.Build.Devcontainer.DevContainerFilePath)

	config, err := d.readDevcontainerConfig(opts, socketForwardId, configFilePath)
	if err != nil {
		return err
	}

	workspaceFolder := ""

	pattern := `"workspaceFolder":"([^"]+)"`
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(config)

	if len(match) > 1 {
		workspaceFolder = match[1]
	} else {
		return fmt.Errorf("unable to determine workspace folder from devcontainer configuration")
	}

	var devcontainerConfigContent = []byte{}
	if opts.SshSessionConfig != nil {
		devcontainerConfigContent, err = ssh.ReadFile(opts.SshSessionConfig, configFilePath)
	} else {
		devcontainerConfigContent, err = os.ReadFile(configFilePath)
	}
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

	for k, v := range opts.Project.EnvVars {
		envVars[k] = v
	}

	envVars["DAYTONA_PROJECT_DIR"] = workspaceFolder

	devcontainerConfig["containerEnv"] = envVars

	configString, err := json.MarshalIndent(devcontainerConfig, "", "  ")
	if err != nil {
		return err
	}

	devcontainerCmd := []string{
		"devcontainer",
		"up",
		"--workspace-folder=" + opts.ProjectDir,
		"--config=" + configFilePath,
		"--override-config=/tmp/daytona-devcontainer.json",
		"--id-label=daytona.workspace.id=" + opts.Project.WorkspaceId,
		"--id-label=daytona.project.name=" + opts.Project.Name,
	}

	if prebuild {
		devcontainerCmd = append(devcontainerCmd, "--prebuild")
	}

	cmd := []string{"-c", fmt.Sprintf("echo '%s' > /tmp/daytona-devcontainer.json && %s", configString, strings.Join(devcontainerCmd, " "))}

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
				Source: opts.ProjectDir,
				Target: opts.ProjectDir,
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
			err = d.GetContainerLogs(c.ID, opts.LogWriter)
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

	containers, err := d.apiClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", dockerSockForwardContainer)),
	})
	if err != nil {
		return "", err
	}

	if len(containers) > 1 {
		return "", fmt.Errorf("multiple containers with name %s found", dockerSockForwardContainer)
	}

	if len(containers) == 1 {
		return containers[0].ID, nil
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

func (d *DockerClient) readDevcontainerConfig(opts *CreateProjectOptions, socketForwardId, configFilePath string) (string, error) {
	ctx := context.Background()

	devcontainerCmd := []string{
		"devcontainer",
		"read-configuration",
		"--workspace-folder=" + opts.ProjectDir,
		"--config=" + configFilePath,
	}

	cmd := []string{"-c", strings.Join(devcontainerCmd, " ")}

	c, err := d.apiClient.ContainerCreate(ctx, &container.Config{
		Image:      "daytonaio/workspace-project",
		Entrypoint: []string{"sh"},
		Env:        []string{"DOCKER_HOST=tcp://localhost:2375"},
		Cmd:        cmd,
		// AttachStdout: true,
		// AttachStderr: true,
	}, &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(fmt.Sprintf("container:%s", socketForwardId)),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: opts.ProjectDir,
				Target: opts.ProjectDir,
			},
		},
	}, nil, nil, uuid.NewString())
	if err != nil {
		return "", err
	}

	waitResponse, errChan := d.apiClient.ContainerWait(ctx, c.ID, container.WaitConditionNextExit)

	err = d.apiClient.ContainerStart(ctx, c.ID, container.StartOptions{})
	if err != nil {
		return "", err
	}

	opts.LogWriter.Write([]byte("Wait for container to end...\n"))

	select {
	case err := <-errChan:
		if err != nil {
			return "", err
		}
	case resp := <-waitResponse:
		if resp.StatusCode != 0 {
			return "", fmt.Errorf("container exited with status %d", resp.StatusCode)
		}
		if resp.Error != nil {
			return "", fmt.Errorf("container exited with error: %s", resp.Error.Message)
		}
	}

	config := ""

	r, w := io.Pipe()
	writer := io.MultiWriter(w, opts.LogWriter)

	opts.LogWriter.Write([]byte("Reading devcontainer configuration...\n"))

	err = d.GetContainerLogsNoFollow(c.ID, writer)
	if err != nil {
		return "", err
	}

	opts.LogWriter.Write([]byte("Done reading devcontainer configuration...\n"))

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		config += scanner.Text()
	}

	opts.LogWriter.Write([]byte(config))

	return config[strings.Index(config, "{"):], nil
}
