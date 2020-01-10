// Copyright 2020 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/cespare/xxhash"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/refresh"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

const httpSourceLabel = model.MetaLabelPrefix + "http_source_url"

// SDConfig is the configuration for file based discovery.
type SDConfig struct {
	URL              config.URL              `yaml:"url"`
	HTTPClientConfig config.HTTPClientConfig `yaml:",inline"`
	RefreshInterval  model.Duration          `yaml:"refresh_interval,omitempty"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface
func (c *SDConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = SDConfig{}
	type plain SDConfig
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}
	err = c.HTTPClientConfig.Validate()
	if err != nil {
		return err
	}

	err = c.HTTPClientConfig.Validate()

	if c.URL.URL == nil {
		return errors.Errorf("url is required")
	}

	if c.RefreshInterval == 0 {
		c.RefreshInterval = model.Duration(5 * time.Minute)
	}

	return nil
}

// Discovery implements the discoverer interface for discovering
// targets from an HTTP service.
type Discovery struct {
	*refresh.Discovery
	url         *url.URL
	client      *http.Client
	lastRefresh map[string]bool
	etag        string
}

// NewDiscovery creates a new HTTP discovery.
func NewDiscovery(conf *SDConfig, logger log.Logger) (*Discovery, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	rt, err := config.NewRoundTripperFromConfig(conf.HTTPClientConfig, "http_sd", false)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: rt,
	}

	d := &Discovery{
		url:         conf.URL.URL,
		client:      client,
		lastRefresh: make(map[string]bool),
	}

	d.Discovery = refresh.NewDiscovery(
		logger,
		"dns",
		time.Duration(conf.RefreshInterval),
		d.refresh,
	)

	return d, nil
}

func (d *Discovery) refresh(ctx context.Context) ([]*targetgroup.Group, error) {
	u := d.url.String()

	req := &http.Request{
		Method:     http.MethodGet,
		URL:        d.url,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       d.url.Host,
	}

	req = req.WithContext(ctx)

	if d.etag != "" {
		req.Header.Set("If-None-Match", d.etag)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "http_sd: failed to get url %s", u)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("http_sd: unexpected HTTP status from url %s", u)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "http_sd: failed to read body from url %s", u)
	}

	var targetGroups []*targetgroup.Group

	if err := json.Unmarshal(body, &targetGroups); err != nil {
		return nil, errors.Wrapf(err, "http_sd: failed to parse body from url %s", u)
	}

	ref := map[string]bool{}

	for _, tg := range targetGroups {
		if tg.Labels == nil {
			tg.Labels = model.LabelSet{}
		}
		tg.Labels[httpSourceLabel] = model.LabelValue(u)
		tg.Source = fmt.Sprintf("%s:%d", u, hashForTargetGroup(tg))
		ref[tg.Source] = true
	}

	for k := range d.lastRefresh {
		_, ok := ref[k]
		if ok {
			// remove group
			tg := targetgroup.Group{Source: k}
			targetGroups = append(targetGroups, &tg)
		}
	}

	return targetGroups, nil
}

const sep = '\xff'

func hashForTargetGroup(tg *targetgroup.Group) uint64 {
	var hosts []string
	for _, t := range tg.Targets {
		hosts = append(hosts, string(t[model.AddressLabel]))
	}

	sort.Strings(hosts)

	b := make([]byte, 0, 1024)

	for _, h := range hosts {
		b = append(b, h...)
		b = append(b, sep)
	}
	return xxhash.Sum64(b)
}
