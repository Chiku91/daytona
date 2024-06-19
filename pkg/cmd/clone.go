// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"os"

	"github.com/daytonaio/daytona/pkg/git"
	"github.com/daytonaio/daytona/pkg/workspace"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	projectDir string
	username   string
	password   string
)

var cloneCmd = &cobra.Command{
	Use:    "clone [PROJECT_JSON]",
	Hidden: true,
	Short:  "Clone a project repository with provided credentials",
	Args:   cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var project workspace.Project
		err := json.Unmarshal([]byte(args[0]), &project)
		if err != nil {
			log.Fatal(err)
		}

		gitservice := &git.Service{
			ProjectDir: projectDir,
			LogWriter:  os.Stdout,
		}

		var auth *http.BasicAuth
		if username != "" && password != "" {
			auth = &http.BasicAuth{
				Username: username,
				Password: password,
			}
		}

		err = gitservice.CloneRepository(&project, auth)
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	cloneCmd.Flags().StringVarP(&projectDir, "project-dir", "d", "", "The project directory")
	cloneCmd.Flags().StringVarP(&username, "username", "u", "", "The username")
	cloneCmd.Flags().StringVarP(&password, "password", "p", "", "The password")
}
