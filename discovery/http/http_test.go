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
		/*{
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
			body:          "[]",
			expectedError: `cannot unmarshal array into Go value of type struct`,
		},*/
		{
			name:     "empty",
			body:     `{}`,
			expected: []*targetgroup.Group{{}},
		},
		{
			name: "two tagets",
			body: `{"targets": [ "somehost:8080", "anotherhost:9090" ]}`,
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
			//t.Parallel()
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

			fillInTargetGroups(s.URL, tc.expected)

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

func fillInTargetGroups(u string, tgs []*targetgroup.Group) {
	for _, tg := range tgs {
		tg.Source = u

		if len(tg.Targets) == 0 {
			continue
		}
		if tg.Labels == nil {
			tg.Labels = model.LabelSet{}
		}
		tg.Labels[httpSourceLabel] = model.LabelValue(u)
	}
}
