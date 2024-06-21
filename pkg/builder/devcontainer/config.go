// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package devcontainer

import (
	"fmt"
)

func ConvertCommands(mergedCommands []interface{}) ([]string, error) {
	// Convert the commands to a string array
	var commandArray []string
	for _, commands := range mergedCommands {
		switch commands := commands.(type) {
		case []interface{}:
			for _, command := range commands {
				commandString, ok := command.(string)
				if !ok {
					return nil, fmt.Errorf("invalid command type: %v", command)
				}
				commandArray = append(commandArray, commandString)
			}
		case map[string]interface{}:
			for _, command := range commands {
				commandString, ok := command.(string)
				if !ok {
					return nil, fmt.Errorf("invalid command type: %v", command)
				}
				commandArray = append(commandArray, commandString)
			}
		case string:
			commandArray = append(commandArray, commands)
		default:
			return nil, fmt.Errorf("invalid command type")
		}
	}

	return commandArray, nil
}
