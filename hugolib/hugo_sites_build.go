// Copyright 2016-present The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hugolib

import (
	"time"

	"errors"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/hugo/helpers"
)

// Build builds all sites. If filesystem events are provided,
// this is considered to be a potential partial rebuild.
func (h *HugoSites) Build(config BuildCfg, events ...fsnotify.Event) error {
	t0 := time.Now()

	// Need a pointer as this may be modified.
	conf := &config

	if conf.whatChanged == nil {
		// Assume everything has changed
		conf.whatChanged = &whatChanged{source: true, other: true}
	}

	if len(events) > 0 {
		// Rebuild
		if err := h.initRebuild(conf); err != nil {
			return err
		}
	} else {
		if err := h.init(conf); err != nil {
			return err
		}
	}

	if err := h.process(conf, events...); err != nil {
		return err
	}

	if err := h.assemble(conf); err != nil {
		return err
	}

	if err := h.render(conf); err != nil {
		return err
	}

	if config.PrintStats {
		h.Log.FEEDBACK.Printf("total in %v ms\n", int(1000*time.Since(t0).Seconds()))
	}

	return nil

}

// Build lifecycle methods below.
// The order listed matches the order of execution.

func (h *HugoSites) init(config *BuildCfg) error {

	for _, s := range h.Sites {
		if s.PageCollections == nil {
			s.PageCollections = newPageCollections()
		}
	}

	if config.ResetState {
		h.reset()
	}

	if config.CreateSitesFromConfig {
		if err := h.createSitesFromConfig(); err != nil {
			return err
		}
	}

	h.runMode.Watching = config.Watching

	return nil
}

func (h *HugoSites) initRebuild(config *BuildCfg) error {
	if config.CreateSitesFromConfig {
		return errors.New("Rebuild does not support 'CreateSitesFromConfig'.")
	}

	if config.ResetState {
		return errors.New("Rebuild does not support 'ResetState'.")
	}

	if !config.Watching {
		return errors.New("Rebuild called when not in watch mode")
	}

	h.runMode.Watching = config.Watching

	for _, s := range h.Sites {
		s.resetBuildState()
	}

	helpers.InitLoggers()

	return nil
}

func (h *HugoSites) process(config *BuildCfg, events ...fsnotify.Event) error {
	// We should probably refactor the Site and pull up most of the logic from there to here,
	// but that seems like a daunting task.
	// So for now, if there are more than one site (language),
	// we pre-process the first one, then configure all the sites based on that.

	firstSite := h.Sites[0]

	if len(events) > 0 {
		// This is a rebuild
		changed, err := firstSite.reProcess(events)
		config.whatChanged = &changed
		return err
	}

	return firstSite.process(*config)

}

func (h *HugoSites) assemble(config *BuildCfg) error {
	if config.whatChanged.source {
		for _, s := range h.Sites {
			s.createTaxonomiesEntries()
		}
	}

	// TODO(bep) we could probably wait and do this in one go later
	h.setupTranslations()

	if len(h.Sites) > 1 {
		// The first is initialized during process; initialize the rest
		for _, site := range h.Sites[1:] {
			site.initializeSiteInfo()
		}
	}

	if config.whatChanged.source {
		h.assembleGitInfo()

		for _, s := range h.Sites {
			if err := s.buildSiteMeta(); err != nil {
				return err
			}
		}
	}

	if err := h.createMissingPages(); err != nil {
		return err
	}

	for _, s := range h.Sites {
		s.refreshPageCaches()
		s.setupSitePages()
	}

	if err := h.assignMissingTranslations(); err != nil {
		return err
	}

	for _, s := range h.Sites {
		s.preparePagesForRender(config)
	}

	return nil

}

func (h *HugoSites) render(config *BuildCfg) error {
	if !config.SkipRender {
		for _, s := range h.Sites {
			if err := s.render(); err != nil {
				return err
			}

			if config.PrintStats {
				s.Stats()
			}
		}

		if err := h.renderCrossSitesArtifacts(); err != nil {
			return err
		}
	}

	return nil
}
