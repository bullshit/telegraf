package newrelic

import (
	ejson "encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/influxdata/telegraf/testutil"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	fakelicence        = "dummy"
	fakehostname       = "testhostname"
	responseOK         = `{"status": "ok"}`
	responseForceError = `{"error":"force error"}`
)

var (
	m1, _ = metric.New("m1",
		map[string]string{"tag1": "tagvalue1"},
		map[string]interface{}{
			"value1": float64(3),
			"value2": float64(4),
		},
		time.Now(),
	)
	m2, _ = metric.New("m2",
		map[string]string{"tag2": "tagvalue2"},
		map[string]interface{}{
			"v1": float64(6),
			"v2": float64(8),
		},
		time.Now(),
	)
	m3, _ = metric.New("m1",
		map[string]string{"tag1": "tagvalue1"},
		map[string]interface{}{
			"value1": float64(2),
			"value2": float64(9),
		},
		time.Now(),
	)
	m4, _ = metric.New("m1",
		map[string]string{"tag1": "tagvalue2"},
		map[string]interface{}{
			"value1": float64(1),
			"value2": float64(2),
		},
		time.Now(),
	)
	m5, _ = metric.New("m1",
		map[string]string{"tag1": "tagvalue1"},
		map[string]interface{}{
			"asdf1": float64(3),
			"asdf2": float64(4),
		},
		time.Now(),
	)
)

func initServer(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contenttype := r.Header.Get("Content-Type")
		accept := r.Header.Get("Accept")
		license := r.Header.Get("X-License-Key")
		assert.Equal(t, "application/json", contenttype)
		assert.Equal(t, "application/json", accept)
		assert.NotEmpty(t, license)
		w.Header().Set("Content-Type", "application/json")
		if license == fakelicence {
			if body, err := ioutil.ReadAll(r.Body); err == nil {
				hostname, _ := os.Hostname()
				var pid = os.Getpid()
				var expectedTpl = `{
					"agent": {
						"host": "#HOSTNAME#",
						"pid": #PID#,
						"version": "1.0.0"
					},
					"components": [
						{
							"duration": "60",
							"guid": "test.sonica.telegraf",
							"name": "#HOSTNAME#",
							"metrics": {
								"Component/test1/value1/value":  {
									"count": 1,
									"total": 1.0,
									"min": 1.0,
									"max": 1.0,
									"sum_of_squares": 1.0
								}
							}
						}
					]
				}`
				var hostnameReplacer = strings.NewReplacer("#HOSTNAME#", hostname, "#PID#", strconv.Itoa(pid))
				var expected = hostnameReplacer.Replace(expectedTpl)
				require.JSONEq(t, expected, fmt.Sprintf("%s", body))
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, responseOK)
			} else {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, responseForceError)
			}
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	return server
}

func TestRequestSerialize(t *testing.T) {
	var request NRRequest

	var c NRComponent
	c.GUID = GUID
	c.Duration = 60
	c.Name = fakehostname

	c.Metrics = make(map[string]NRMetric, 1)
	c.Metrics["Component/test1/value1/value"] = NRMetric{Count: 1, Total: 1.0, Min: 1.0, Max: 1.0, SumOfSquares: 1.0}

	request.Agent.Host = fakehostname
	request.Agent.PID = 42
	request.Agent.Version = "1.0.0"
	request.Components = append(request.Components, c)

	buf, err := ejson.Marshal(request)
	require.NoError(t, err)

	var expected = `{
		"agent": {
			"host": "testhostname",
			"pid": 42,
			"version": "1.0.0"
		},
		"components": [
			{
				"duration": "60",
				"guid": "test.sonica.telegraf",
				"name": "testhostname",
				"metrics": {
					"Component/test1/value1/value": {
						"count": 1,
						"total": 1.0,
						"min": 1.0,
						"max": 1.0,
						"sum_of_squares": 1.0
					}
				}
			}
		]
	}`

	require.JSONEq(t, expected, fmt.Sprintf("%s", buf))
}

func TestLicenceKeyHeader(t *testing.T) {
	/*if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}*/
	server := initServer(t)
	defer server.Close()

	i := NewRelic{
		URL:     server.URL,
		License: "nolicense",
	}

	err := i.Connect()
	metrics := testutil.MockMetrics()
	err = i.Write(metrics)
	require.Error(t, err)
}

func TestWrite(t *testing.T) {
	/*if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}*/
	server := initServer(t)
	defer server.Close()

	i := NewRelic{
		URL:     server.URL,
		License: fakelicence,
	}

	err := i.Connect()
	metrics := testutil.MockMetrics()
	err = i.Write(metrics)
	require.NoError(t, err)
}

func TestMultiplyWrite(t *testing.T) {
	/*if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}*/
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, err := ioutil.ReadAll(r.Body); err == nil {
			hostname, _ := os.Hostname()
			var pid = os.Getpid()
			var expectedTpl = `{
				"agent": {
					"host": "#HOSTNAME#",
					"pid": #PID#,
					"version": "1.0.0"
				},
				"components": [
					{
						"duration": "60",
						"guid": "test.sonica.telegraf",
						"name": "#HOSTNAME#",
						"metrics": {
							"Component/m1/tagvalue1/value1":  {
								"total": 5,
								"count" : 2,
								"min": 2,
								"max": 3,
								"sum_of_squares": 13
							},
							"Component/m1/tagvalue1/value2":  {
								"total": 13,
								"count" : 2,
								"min": 4,
								"max": 9,
								"sum_of_squares":97
							},
							"Component/m1/tagvalue2/value1": {
								"total": 1,
								"count" : 1,
								"min": 1,
								"max": 1,
								"sum_of_squares": 1
							},
							"Component/m1/tagvalue2/value2": {
								"total": 2,
								"count" : 1,
								"min": 2,
								"max": 2,
								"sum_of_squares": 4
							},
							"Component/m1/tagvalue1/asdf1": {
								"total": 3,
								"count" : 1,
								"min": 3,
								"max": 3,
								"sum_of_squares":9
							},
							"Component/m1/tagvalue1/asdf2": {
								"total": 4,
								"count" : 1,
								"min": 4,
								"max": 4,
								"sum_of_squares":16
							}
						}
					},
					{
						"duration": "60",
						"guid": "test.sonica.telegraf",
						"name": "#HOSTNAME#",
						"metrics": {
							"Component/m2/tagvalue2/v1": {
								"total": 6,
								"count" : 1,
								"min": 6,
								"max": 6,
								"sum_of_squares": 36
							},
							"Component/m2/tagvalue2/v2":  {
								"total": 8,
								"count" : 1,
								"min": 8,
								"max": 8,
								"sum_of_squares": 64
							}
						}
					}
				]
			}`
			var hostnameReplacer = strings.NewReplacer("#HOSTNAME#", hostname, "#PID#", strconv.Itoa(pid))
			var expected = hostnameReplacer.Replace(expectedTpl)
			require.JSONEq(t, expected, fmt.Sprintf("%s", body))
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, responseOK)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, responseForceError)
		}
	}))
	defer s.Close()

	i := NewRelic{
		URL:     s.URL,
		License: fakelicence,
	}

	err := i.Connect()
	metrics := []telegraf.Metric{m1, m2, m3, m4, m5}
	err = i.Write(metrics)
	require.NoError(t, err)
}

func TestForceError(t *testing.T) {
	/*if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}*/
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, responseForceError)
	}))
	defer s.Close()

	i := NewRelic{
		URL:     s.URL,
		License: fakelicence,
	}

	err := i.Connect()
	metrics := testutil.MockMetrics()
	err = i.Write(metrics)
	require.Error(t, err)
}
