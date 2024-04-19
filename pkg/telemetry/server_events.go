// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"context"
	"net/http"
	"time"

	"github.com/daytonaio/daytona/pkg/provider"
	"github.com/daytonaio/daytona/pkg/workspace"
)

type ServerEvent string

const (
	ServerEventApiRequestStarted ServerEvent = "server_api_request_started"
	ServerEventApiResponseSent   ServerEvent = "server_api_response_sent"

	// Workspace events
	ServerEventWorkspaceCreated      ServerEvent = "server_workspace_created"
	ServerEventWorkspaceDestroyed    ServerEvent = "server_workspace_destroyed"
	ServerEventWorkspaceStarted      ServerEvent = "server_workspace_started"
	ServerEventWorkspaceStopped      ServerEvent = "server_workspace_stopped"
	ServerEventWorkspaceCreateError  ServerEvent = "server_workspace_created_error"
	ServerEventWorkspaceDestroyError ServerEvent = "server_workspace_destroyed_error"
	ServerEventWorkspaceStartError   ServerEvent = "server_workspace_started_error"
	ServerEventWorkspaceStopError    ServerEvent = "server_workspace_stopped_error"
)

func NewWorkspaceEventProps(workspace *workspace.Workspace, target *provider.ProviderTarget) map[string]interface{} {
	props := map[string]interface{}{}

	if workspace != nil {
		props["workspace_n_projects"] = len(workspace.Projects)
		publicRepos := []string{}
		images := []string{}

		for _, project := range workspace.Projects {
			images = append(images, project.Image)
			if project.Repository != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				_, err := http.NewRequestWithContext(ctx, "HEAD", project.Repository.Url, nil)
				cancel()
				if err == nil {
					publicRepos = append(publicRepos, project.Repository.Url)
				}
			}
		}

		props["workspace_public_repos"] = publicRepos
		props["workspace_images"] = images
	}

	if target != nil {
		props["target_name"] = target.Name
		props["target_provider"] = target.ProviderInfo.Name
		props["target_provider_version"] = target.ProviderInfo.Version
	}

	return props
}
