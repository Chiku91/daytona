// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

func (d *DockerClient) startDevcontainerProject(opts *CreateProjectOptions) (RemoteUser, error) {
	return d.createProjectFromDevcontainer(opts, false)
}
