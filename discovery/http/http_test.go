// Copyright 2020 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/util/testutil"
)

func TestHTTP(t *testing.T) {
	testCases := []struct {
		name          string
		body          string
		httpStatus    int
		expectedError string
		expected      []*targetgroup.Group
	}{
		{
			name:          "empty body",
			expectedError: "unexpected end of JSON input",
		},
		{
			name:          "not found",
			httpStatus:    http.StatusNotFound,
			expectedError: "unexpected HTTP status",
		},
		{
			name:          "invalid json",
			body:          "{}",
			expectedError: `cannot unmarshal object into Go value of type []`,
		},
		{
			name:     "empty array",
			body:     `[]`,
			expected: []*targetgroup.Group{},
		},
		{
			name: "single target group",
			body: `[ {"targets": [ "somehost:8080", "anotherhost:9090" ] }]`,
			expected: []*targetgroup.Group{
				{
					Targets: []model.LabelSet{
						{
							model.AddressLabel: model.LabelValue("somehost:8080"),
						},
						{
							model.AddressLabel: model.LabelValue("anotherhost:9090"),
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.httpStatus != http.StatusOK && tc.httpStatus != 0 {
					w.WriteHeader(tc.httpStatus)
					return
				}
				w.WriteHeader(http.StatusOK)

				_, _ = w.Write([]byte(tc.body))
			})

			s := httptest.NewServer(handler)
			defer s.Close()

			for _, tg := range tc.expected {
				if tg.Labels == nil {
					tg.Labels = model.LabelSet{}
				}
				tg.Labels[httpSourceLabel] = model.LabelValue(s.URL)
				tg.Source = fmt.Sprintf("%s:%d", s.URL, hashForTargetGroup(tg))
			}

			u, err := url.Parse(s.URL)
			testutil.Ok(t, err)

			conf := SDConfig{
				URL: config.URL{
					URL: u,
				},
			}

			sd, err := NewDiscovery(&conf, nil)
			testutil.Ok(t, err)

			tgs, err := sd.refresh(context.Background())

			if tc.expectedError != "" {
				testutil.NotOk(t, err)
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Fatal("error did not contain expected text")
				}
				return
			}

			testutil.Ok(t, err)
			testutil.Equals(t, tc.expected, tgs)
		})
	}

}

func TestHTTPAddAndDelete(t *testing.T) {
	testCases := []struct {
		name      string
		responses []string
		expected  [][]*targetgroup.Group
	}{
		{
			name: "same response",
			responses: []string{
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ] }]`,
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ] }]`,
			},
			expected: [][]*targetgroup.Group{
				{
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("somehost:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("anotherhost:9090"),
							},
						},
					},
				},
				{
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("somehost:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("anotherhost:9090"),
							},
						},
					},
				},
			},
		},
		{
			name: "remove group",
			responses: []string{
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ] }, {"targets": [ "yetanother:8080"] }]`,
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ] }]`,
			},
			expected: [][]*targetgroup.Group{
				{
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("somehost:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("anotherhost:9090"),
							},
						},
					},
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("yetanother:8080"),
							},
						},
					},
				},
				{
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("somehost:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("anotherhost:9090"),
							},
						},
					},
				},
			},
		},
		{
			name: "add group",
			responses: []string{
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ] }]`,
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ] }, {"targets": [ "yetanother:8080"] }]`,
			},
			expected: [][]*targetgroup.Group{
				{
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("somehost:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("anotherhost:9090"),
							},
						},
					},
				},
				{
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("somehost:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("anotherhost:9090"),
							},
						},
					},
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue("yetanother:8080"),
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, resp := range tc.responses {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

					w.WriteHeader(http.StatusOK)

					_, _ = w.Write([]byte(resp))
				})

				s := httptest.NewServer(handler)
				defer s.Close()

				expected := tc.expected[i]
				for _, tg := range expected {
					if tg.Labels == nil {
						tg.Labels = model.LabelSet{}
					}
					tg.Labels[httpSourceLabel] = model.LabelValue(s.URL)
					tg.Source = fmt.Sprintf("%s:%d", s.URL, hashForTargetGroup(tg))
				}

				u, err := url.Parse(s.URL)
				testutil.Ok(t, err)

				conf := SDConfig{
					URL: config.URL{
						URL: u,
					},
				}

				sd, err := NewDiscovery(&conf, nil)
				testutil.Ok(t, err)

				tgs, err := sd.refresh(context.Background())

				testutil.Ok(t, err)
				testutil.Equals(t, expected, tgs)
			}
		})
	}
}
