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
		bodies        []string
		httpStatus    int
		expectedError string
		expected      [][]*targetgroup.Group
	}{
		{
			name:          "empty body",
			httpStatus:    http.StatusOK,
			expectedError: "unexpected end of JSON input",
		},
		{
			name:          "not found",
			httpStatus:    http.StatusNotFound,
			expectedError: "unexpected HTTP status",
		},
		{
			name:          "invalid json",
			bodies:        []string{"{}"},
			httpStatus:    http.StatusOK,
			expectedError: `cannot unmarshal object into Go value`,
		},
		{
			name:       "empty",
			bodies:     []string{`[]`},
			httpStatus: http.StatusOK,
			expected:   [][]*targetgroup.Group{{}},
		},
		{
			name:       "one target group",
			bodies:     []string{`[ {"targets": [ "somehost:8080", "anotherhost:9090" ]} ]`},
			httpStatus: http.StatusOK,
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
			},
		},
		{
			name: "add target group",
			bodies: []string{
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ]} ]`,
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ]}, {"targets": [ "abc:8080", "xyz:9090" ]} ]`,
			},
			httpStatus: http.StatusOK,
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
								model.AddressLabel: model.LabelValue("abc:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("xyz:9090"),
							},
						},
					},
				},
			},
		},
		{
			name: "remove target group",
			bodies: []string{
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ]}, {"targets": [ "abc:8080", "xyz:9090" ]} ]`,
				`[ {"targets": [ "somehost:8080", "anotherhost:9090" ]} ]`,
			},
			httpStatus: http.StatusOK,
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
								model.AddressLabel: model.LabelValue("abc:8080"),
							},
							{
								model.AddressLabel: model.LabelValue("xyz:9090"),
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
					{},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			//t.Parallel()

			handler := &testHandler{
				statusCode: tc.httpStatus,
			}

			s := httptest.NewServer(handler)
			defer s.Close()

			u, err := url.Parse(s.URL)
			testutil.Ok(t, err)

			conf := SDConfig{
				URL: config.URL{
					URL: u,
				},
			}

			sd, err := NewDiscovery(&conf, nil)
			testutil.Ok(t, err)

			for i, body := range tc.bodies {
				handler.body = body

				tgs, err := sd.refresh(context.Background())

				if tc.expectedError != "" {
					testutil.NotOk(t, err)
					if !strings.Contains(err.Error(), tc.expectedError) {
						t.Fatal("error did not contain expected text")
					}
					return
				}

				expected := tc.expected[i]

				fillInTargetGroups(s.URL, expected)

				testutil.Ok(t, err)
				testutil.Equals(t, len(expected), len(tgs))

				for i := range expected {
					testutil.Equals(t, expected[i], tgs[i])
				}
			}

		})
	}

}

func fillInTargetGroups(u string, tgs []*targetgroup.Group) {
	for i, tg := range tgs {
		tg.Source = fmt.Sprintf("%s:%d", u, i)

		if len(tg.Targets) == 0 {
			continue
		}
		if tg.Labels == nil {
			tg.Labels = model.LabelSet{}
		}
		tg.Labels[httpSourceLabel] = model.LabelValue(u)
	}
}

type testHandler struct {
	statusCode int
	body       string
}

func (t *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(t.statusCode)

	if t.body != "" {
		_, _ = w.Write([]byte(t.body))
	}
}
