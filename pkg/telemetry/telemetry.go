// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package telemetry

import "io"

const ENABLED_HEADER = "X-Daytona-Telemetry-Enabled"
const SESSION_ID_HEADER = "X-Daytona-Session-Id"

type TelemetryService interface {
	io.Closer
	TrackCliEvent(event CliEvent, sessionId string, properties map[string]interface{}) error
	TrackServerEvent(event ServerEvent, sessionId string, properties map[string]interface{}) error
}
