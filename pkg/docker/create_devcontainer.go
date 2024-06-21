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
	"path"
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

type RemoteUser string

func (d *DockerClient) createProjectFromDevcontainer(opts *CreateProjectOptions, prebuild bool) (RemoteUser, error) {
	socketForwardId, err := d.ensureDockerSockForward(opts.LogWriter)
	if err != nil {
		return "", err
	}

	ctx := context.Background()

	mountTarget := path.Join("/workdir", filepath.Base(opts.ProjectDir))
	targetConfigFilePath := path.Join(mountTarget, opts.Project.Build.Devcontainer.DevContainerFilePath)

	config, err := d.readDevcontainerConfig(opts, socketForwardId, targetConfigFilePath)
	if err != nil {
		return "", err
	}

	workspaceFolder := d.getDevcontainerConfigProp(config, "workspaceFolder")
	if workspaceFolder == "" {
		return "", fmt.Errorf("unable to determine workspace folder from devcontainer configuration")
	}

	remoteUser := d.getDevcontainerConfigProp(config, "remoteUser")

	if remoteUser == "" {
		return "", fmt.Errorf("unable to determine remote user from devcontainer configuration")
	}

	var devcontainerConfigContent = []byte{}
	if opts.SshSessionConfig != nil {
		configFilePath := path.Join(opts.ProjectDir, opts.Project.Build.Devcontainer.DevContainerFilePath)
		devcontainerConfigContent, err = ssh.ReadFile(opts.SshSessionConfig, configFilePath)
	} else {
		configFilePath := filepath.Join(opts.ProjectDir, opts.Project.Build.Devcontainer.DevContainerFilePath)
		devcontainerConfigContent, err = os.ReadFile(configFilePath)
	}
	if err != nil {
		return "", err
	}

	var devcontainerConfig map[string]interface{}

	standardized, err := hujson.Standardize(devcontainerConfigContent)
	if err != nil {
		return "", err
	}

	err = json.Unmarshal(standardized, &devcontainerConfig)
	if err != nil {
		return "", err
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

	// If the workspaceFolder is not set in the devcontainer.json, we set it to /workspaces/<project-name>
	if _, ok := devcontainerConfig["workspaceFolder"].(string); !ok {
		workspaceFolder = fmt.Sprintf("/workspaces/%s", opts.Project.Name)
		devcontainerConfig["workspaceFolder"] = workspaceFolder
	}
	devcontainerConfig["workspaceMount"] = fmt.Sprintf("source=%s,target=%s,type=bind", opts.ProjectDir, workspaceFolder)

	composeOverrideCmd := []string{}
	if _, ok := devcontainerConfig["dockerComposeFile"]; ok {
		composeFilePath := devcontainerConfig["dockerComposeFile"].(string)
		var composeFileContent []byte
		if opts.SshSessionConfig != nil {
			composeFilePath := path.Join(opts.ProjectDir, filepath.Dir(opts.Project.Build.Devcontainer.DevContainerFilePath), composeFilePath)
			composeFileContent, err = ssh.ReadFile(opts.SshSessionConfig, composeFilePath)
		} else {
			composeFilePath := filepath.Join(opts.ProjectDir, filepath.Dir(opts.Project.Build.Devcontainer.DevContainerFilePath), composeFilePath)
			composeFileContent, err = os.ReadFile(composeFilePath)
		}
		if err != nil {
			return "", err
		}

		overrideComposeContent := strings.ReplaceAll(string(composeFileContent), "- ..:", fmt.Sprintf("- %s:", opts.ProjectDir))
		overrideComposeContent = strings.ReplaceAll(overrideComposeContent, "'", `'"'"'`)
		overrideComposeContent = strings.ReplaceAll(overrideComposeContent, "context: .", fmt.Sprintf("context: %s", path.Join(mountTarget, filepath.Dir(opts.Project.Build.Devcontainer.DevContainerFilePath))))

		composeOverrideCmd = []string{
			"echo",
			fmt.Sprintf(`'%s'`, overrideComposeContent),
			">",
			"/tmp/daytona-compose-override.yml",
			"&&",
		}
		devcontainerConfig["dockerComposeFile"] = "/tmp/daytona-compose-override.yml"
	}

	envVars["DAYTONA_PROJECT_DIR"] = workspaceFolder

	devcontainerConfig["containerEnv"] = envVars

	configString, err := json.MarshalIndent(devcontainerConfig, "", "  ")
	if err != nil {
		return "", err
	}

	devcontainerCmd := append(composeOverrideCmd, []string{
		"devcontainer",
		"up",
		"--workspace-folder=" + mountTarget,
		"--config=" + targetConfigFilePath,
		"--override-config=/tmp/daytona-devcontainer.json",
		"--id-label=daytona.workspace.id=" + opts.Project.WorkspaceId,
		"--id-label=daytona.project.name=" + opts.Project.Name,
	}...)

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
				Target: mountTarget,
			},
		},
	}, nil, nil, uuid.NewString())
	if err != nil {
		return "", err
	}

	// defer d.removeContainer(c.ID)

	waitResponse, errChan := d.apiClient.ContainerWait(ctx, c.ID, container.WaitConditionNextExit)

	err = d.apiClient.ContainerStart(ctx, c.ID, container.StartOptions{})
	if err != nil {
		return "", err
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
			return "", err
		}
	case resp := <-waitResponse:
		if resp.StatusCode != 0 {
			return "", fmt.Errorf("container exited with status %d", resp.StatusCode)
		}
		if resp.Error != nil {
			return "", fmt.Errorf("container exited with error: %s", resp.Error.Message)
		}

		return RemoteUser(remoteUser), nil
	}

	return RemoteUser(remoteUser), nil
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

	mountTarget := path.Join("/workdir", filepath.Base(opts.ProjectDir))

	devcontainerCmd := []string{
		"devcontainer",
		"read-configuration",
		"--workspace-folder=" + mountTarget,
		"--config=" + configFilePath,
	}

	cmd := []string{"-c", strings.Join(devcontainerCmd, " ")}

	c, err := d.apiClient.ContainerCreate(ctx, &container.Config{
		Image:      "daytonaio/workspace-project",
		Entrypoint: []string{"sh"},
		Env:        []string{"DOCKER_HOST=tcp://localhost:2375"},
		Cmd:        cmd,
		Tty:        true,
	}, &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(fmt.Sprintf("container:%s", socketForwardId)),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: opts.ProjectDir,
				Target: mountTarget,
			},
		},
	}, nil, nil, uuid.NewString())
	if err != nil {
		return "", err
	}

	defer d.removeContainer(c.ID)

	waitResponse, errChan := d.apiClient.ContainerWait(ctx, c.ID, container.WaitConditionNextExit)

	opts.LogWriter.Write([]byte("Reading devcontainer configuration...\n"))

	err = d.apiClient.ContainerStart(ctx, c.ID, container.StartOptions{})
	if err != nil {
		return "", err
	}

	output := ""

	r, w := io.Pipe()
	writer := io.MultiWriter(w, opts.LogWriter)

	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			output += scanner.Text()
		}
	}()

	go func() {
		err = d.GetContainerLogs(c.ID, writer)
		if err != nil {
			opts.LogWriter.Write([]byte(fmt.Sprintf("Error reading devcontainer configuration: %v\n", err)))
		}
	}()

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

	configStartIndex := strings.Index(output, "{")
	if configStartIndex == -1 {
		return "", fmt.Errorf("unable to find start of JSON in devcontainer configuration")
	}

	return output[configStartIndex:], nil
}

func (d *DockerClient) getDevcontainerConfigProp(devcontainerConfig, prop string) string {
	pattern := fmt.Sprintf(`"%s":"([^"]+)"`, prop)
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(devcontainerConfig)

	if len(match) > 1 {
		return match[1]
	}

	return ""
}
