/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package pony adds pony images to the issue or PR in response to a /pony comment
package pony

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/config"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/pluginhelp"
	"sigs.k8s.io/prow/pkg/plugins"
)

// Only the properties we actually use.
type ponyResult struct {
	Pony ponyResultPony `json:"pony"`
}

type ponyResultPony struct {
	Representations ponyRepresentations `json:"representations"`
}

type ponyRepresentations struct {
	Full  string `json:"full"`
	Small string `json:"small"`
}

const (
	ponyURL    = realHerd("https://theponyapi.com/api/v1/pony/random")
	pluginName = "pony"
	maxPonies  = 5
)

var (
	match = regexp.MustCompile(`(?mi)^/(?:pony)(?: +(.+?))?\s*$`)
)

func init() {
	plugins.RegisterGenericCommentHandler(pluginName, handleGenericComment, helpProvider)
}

func helpProvider(config *plugins.Configuration, _ []config.OrgRepo) (*pluginhelp.PluginHelp, error) {
	// The Config field is omitted because this plugin is not configurable.
	pluginHelp := &pluginhelp.PluginHelp{
		Description: "The pony plugin adds a pony image to an issue or PR in response to the `/pony` command.",
	}
	pluginHelp.AddCommand(pluginhelp.Command{
		Usage:       "/(pony) [pony]",
		Description: "Add a little pony image to the issue or PR. A particular pony can optionally be named for a picture of that specific pony.",
		Featured:    false,
		WhoCanUse:   "Anyone",
		Examples:    []string{"/pony", "/pony Twilight Sparkle"},
	})
	return pluginHelp, nil
}

var client = http.Client{}

type githubClient interface {
	CreateComment(owner, repo string, number int, comment string) error
}

type herd interface {
	readPony(string) (string, error)
}

type realHerd string

func formatURLs(small, full string) string {
	return fmt.Sprintf("[![pony image](%s)](%s)", small, full)
}

func (h realHerd) readPony(tags string) (string, error) {
	uri := string(h) + "?q=" + url.QueryEscape(tags)
	resp, err := client.Get(uri)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("no pony found")
	}
	var a ponyResult
	if err = json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	embedded := a.Pony.Representations.Small
	tooBig, err := github.ImageTooBig(embedded)
	if err != nil {
		return "", fmt.Errorf("couldn't fetch pony for size check: %w", err)
	}
	if tooBig {
		return "", fmt.Errorf("the pony is too big")
	}
	return formatURLs(a.Pony.Representations.Small, a.Pony.Representations.Full), nil
}

func handleGenericComment(pc plugins.Agent, e github.GenericCommentEvent) error {
	return handle(pc.GitHubClient, pc.Logger, &e, ponyURL)
}

func handle(gc githubClient, log *logrus.Entry, e *github.GenericCommentEvent, p herd) error {
	// Only consider new comments.
	if e.Action != github.GenericCommentActionCreated {
		return nil
	}
	// Make sure they are requesting a pony and don't allow requesting more than 'maxPonies' defined.
	mat := match.FindAllStringSubmatch(e.Body, maxPonies)
	if mat == nil {
		return nil
	}

	org := e.Repo.Owner.Login
	repo := e.Repo.Name
	number := e.Number

	var respBuilder strings.Builder
	var tagsSpecified bool
	for _, tag := range mat {
		for i := 0; i < 5; i++ {
			if tag[1] != "" {
				tagsSpecified = true
			}
			resp, err := p.readPony(tag[1])
			if err != nil {
				log.WithError(err).Println("Failed to get a pony")
				continue
			}
			respBuilder.WriteString(resp + "\n")
			break
		}
	}
	if respBuilder.Len() > 0 {
		return gc.CreateComment(org, repo, number, plugins.FormatResponseRaw(e.Body, e.HTMLURL, e.User.Login, respBuilder.String()))
	}

	var msg string
	if tagsSpecified {
		msg = "Couldn't find a pony matching given tag(s)."
	} else {
		msg = "https://theponyapi.com appears to be down"
	}
	if err := gc.CreateComment(org, repo, number, plugins.FormatResponseRaw(e.Body, e.HTMLURL, e.User.Login, msg)); err != nil {
		log.WithError(err).Error("Failed to leave comment")
	}

	return errors.New("could not find a valid pony image")
}
