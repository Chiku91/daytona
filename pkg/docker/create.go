// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"
	"io"
	"os/user"
	"path/filepath"
	"time"

	"github.com/daytonaio/daytona/pkg/builder/devcontainer"
	"github.com/daytonaio/daytona/pkg/ssh"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	log "github.com/sirupsen/logrus"
)

func (d *DockerClient) CreateWorkspace(workspace *workspace.Workspace, logWriter io.Writer) error {
	return nil
}

func (d *DockerClient) CreateProject(opts *CreateProjectOptions) error {
	err := d.cloneProjectRepository(opts)
	if err != nil {
		return err
	}

	if opts.Project.Build != nil && opts.Project.Build.Devcontainer != nil {
		_, err = d.createProjectFromDevcontainer(opts, true)
	} else if devcontainerFilePath, pathError := devcontainer.FindDevcontainerConfigFilePath(opts.ProjectDir); pathError == nil {
		opts.Project.Build.Devcontainer = &workspace.ProjectBuildDevcontainer{
			DevContainerFilePath: devcontainerFilePath,
		}

		_, err = d.createProjectFromDevcontainer(opts, true)
	} else {
		err = d.createProjectFromImage(opts)
	}

	return err
}

func (d *DockerClient) cloneProjectRepository(opts *CreateProjectOptions) error {
	// TODO: The image should be configurable
	err := d.PullImage("alpine/git", nil, opts.LogWriter)
	if err != nil {
		return err
	}

	ctx := context.Background()

	cloneUrl := opts.Project.Repository.Url
	if opts.Gpc != nil {
		cloneUrl = fmt.Sprintf("https://%s:%s@%s", opts.Gpc.Username, opts.Gpc.Token, opts.Project.Repository.Url)
	}

	cloneCmd := []string{"git", "clone", cloneUrl, fmt.Sprintf("/workdir/%s-%s", opts.Project.WorkspaceId, opts.Project.Name)}

	c, err := d.apiClient.ContainerCreate(ctx, &container.Config{
		Image:      "daytonaio/workspace-project",
		Entrypoint: []string{"sleep"},
		Cmd:        []string{"infinity"},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: filepath.Dir(opts.ProjectDir),
				Target: "/workdir",
			},
		},
	}, nil, nil, fmt.Sprintf("git-clone-%s-%s", opts.Project.WorkspaceId, opts.Project.Name))
	if err != nil {
		return err
	}

	defer d.removeContainer(c.ID)

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

	currentUser, err := user.Current()
	if err != nil {
		return err
	}

	containerUser := "daytona"
	newUid := currentUser.Uid
	newGid := currentUser.Gid

	if opts.SshSessionConfig != nil {
		newUid, newGid, err = ssh.GetUserUidGid(opts.SshSessionConfig)
		if err != nil {
			return err
		}
	}

	if newUid == "0" && newGid == "0" {
		containerUser = "root"
	}

	/*
		Patch UID and GID of the user cloning the repository
	*/
	if containerUser != "root" {
		_, err = d.ExecSync(c.ID, types.ExecConfig{
			User: "root",
			Cmd: []string{"sh", "-c", `eval $(sed -n "s/${REMOTE_USER}:[^:]*:\([^:]*\):\([^:]*\):[^:]*:\([^:]*\).*/OLD_UID=\1;OLD_GID=\2;HOME_FOLDER=\3/p" /etc/passwd); \
		eval $(sed -n "s/\([^:]*\):[^:]*:${NEW_UID}:.*/EXISTING_USER=\1/p" /etc/passwd); \
		eval $(sed -n "s/\([^:]*\):[^:]*:${NEW_GID}:.*/EXISTING_GROUP=\1/p" /etc/group); \
		if [ -z "$OLD_UID" ]; then \
			echo "Remote user not found in /etc/passwd ($REMOTE_USER)."; \
		elif [ "$OLD_UID" = "$NEW_UID" -a "$OLD_GID" = "$NEW_GID" ]; then \
			echo "UIDs and GIDs are the same ($NEW_UID:$NEW_GID)."; \
		elif [ "$OLD_UID" != "$NEW_UID" -a -n "$EXISTING_USER" ]; then \
			echo "User with UID exists ($EXISTING_USER=$NEW_UID)."; \
		else \
			if [ "$OLD_GID" != "$NEW_GID" -a -n "$EXISTING_GROUP" ]; then \
				echo "Group with GID exists ($EXISTING_GROUP=$NEW_GID)."; \
				NEW_GID="$OLD_GID"; \
			fi; \
			echo "Updating UID:GID from $OLD_UID:$OLD_GID to $NEW_UID:$NEW_GID."; \
			sed -i -e "s/\(${REMOTE_USER}:[^:]*:\)[^:]*:[^:]*/\1${NEW_UID}:${NEW_GID}/" /etc/passwd; \
			if [ "$OLD_GID" != "$NEW_GID" ]; then \
				sed -i -e "s/\([^:]*:[^:]*:\)${OLD_GID}:/\1${NEW_GID}:/" /etc/group; \
			fi; \
			chown -R $NEW_UID:$NEW_GID $HOME_FOLDER; \
		fi;`},
			Env: []string{
				fmt.Sprintf("REMOTE_USER=%s", containerUser),
				fmt.Sprintf("NEW_UID=%s", newUid),
				fmt.Sprintf("NEW_GID=%s", newGid),
			},
		}, opts.LogWriter)
		if err != nil {
			return err
		}
	}

	res, err := d.ExecSync(c.ID, types.ExecConfig{
		User: containerUser,
		Cmd:  cloneCmd,
	}, opts.LogWriter)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("git clone failed with exit code %d", res.ExitCode)
	}

	return nil
}
