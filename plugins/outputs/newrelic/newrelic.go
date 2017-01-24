package newrelic

import (
	ejson "encoding/json"
	"fmt"
	"os"

	"bytes"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/outputs"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
)

type (
	NewRelic struct {
		URL     string
		License string
		GUID string

		client *http.Client
	}

	NRResponse struct {
		Error  string `json:"error"`
		Status string `json:"status"`
	}

	NRAgent struct {
		Host    string `json:"host"`
		Version string `json:"version"`
		PID     int    `json:"pid,omitempty"` // optional
	}

	NRMetric struct {
		Count        int     `json:"count"`
		Total        float64 `json:"total"`
		Min          float64 `json:"min,omitempty"`            // optional
		Max          float64 `json:"max,omitempty"`            // optional
		SumOfSquares float64 `json:"sum_of_squares,omitempty"` // optional
	}

	NRComponent struct {
		Name     string              `json:"name"`
		GUID     string              `json:"guid"`
		Duration int                 `json:"duration,string"`
		Metrics  map[string]NRMetric `json:"metrics"`
	}

	NRRequest struct {
		Agent      NRAgent       `json:"agent"`
		Components []NRComponent `json:"components"`
	}

	Aggregator struct {
		Tags      string
		Component NRComponent
	}
)

var request NRRequest
var sanitizedChars = strings.NewReplacer("/", "_", " ", "", "%", "Percent", ":", "_", `\`, "_", "[", "", "]", "",
	".", "", "#", "", "_", "")

const (
	newrelic_api = "https://platform-api.newrelic.com/platform/v1/metrics"
	mimetype     = "application/json"
	default_guid   = "com.influxdata.telegraf"
	licence_header = "X-License-Key"
	prefix         = "Component/"
	sampleConfig   = `
## NewRelic license key
  license = ""
  ## Your newrelic plugin identifier
  #guid = "com.influxdata.telegraf"
`
)

func (n *NewRelic) Connect() error {
	if n.URL == "" {
		n.URL = newrelic_api
	}

	if n.License == "" {
		return fmt.Errorf("Licence key is a required field for newrelic output")
	}

	if n.GUID == "" {
		n.GUID = default_guid
	}
	n.client = &http.Client{}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("FAILED to get hostname: %s, %s", hostname, err)
	}

	request.Agent.PID = os.Getpid()
	request.Agent.Version = "1.0.0"
	request.Agent.Host = hostname
	return nil
}

func (n *NewRelic) Close() error {
	return nil
}

func (n *NewRelic) SampleConfig() string {
	return sampleConfig
}

func (n *NewRelic) Description() string {
	return "Send telegraf metrics to newrelic plugin api"
}

func (n *NewRelic) Write(metrics []telegraf.Metric) error {
	if len(metrics) == 0 {
		return nil
	}

	components := n.BuildComponents(&metrics)
	request.Components = components

	return sendData(n, request)
}

func serialize(m *telegraf.Metric) map[string]NRMetric {
	// TODO: use a class
	values := make(map[string]NRMetric)
	tags := buildTags((*m).Tags())
	// todo check m.Type()
	for k, v := range (*m).Fields() {
		var parts []string
		var value float64
		switch t := v.(type) {
		case int:
			value = float64(t)
		case int32:
			value = float64(t)
		case int64:
			value = float64(t)
		case float64:
			value = t
		case bool:
			if t {
				value = 1
			} else {
				value = 0
			}
		default:
			// Skip unsupported type.
			continue
		}

		mv := NRMetric{Total: value, Count: 1, Min: value, Max: value, SumOfSquares: value * value}

		parts = append(parts, sanitizedChars.Replace((*m).Name()))
		if (tags != "") {
			parts = append(parts, tags)
		}
		parts = append(parts, sanitizedChars.Replace(k))

		values[prefix + strings.Join(parts,"/")] = mv
	}

	return values
}

func equalsTags(lookupTable *map[string]Aggregator, search *telegraf.Metric) bool {
	for name, lookup := range *lookupTable {
		if (*search).Name() == name {
			if buildTags((*search).Tags()) == lookup.Tags {
				//if (reflect.DeepEqual((*search).Tags(), lookup.Tags)) {
				return true
			}
		}
	}
	return false
}

func (n *NewRelic) BuildComponents(metrics *[]telegraf.Metric) []NRComponent {
	var aggregator = make(map[string]Aggregator)

	for _, metric := range *metrics {
		name := metric.Name()
		if _, ok := aggregator[name]; ok {
			if equalsTags(&aggregator, &metric) {
				// add metrics values
				for k, v := range serialize(&metric) {
					t, exists := aggregator[name].Component.Metrics[k]
					t.Count += 1
					t.Total += v.Total
					t.SumOfSquares += v.SumOfSquares
					if !exists || v.Total > t.Max {
						t.Max = v.Total
					}
					if !exists || v.Total < t.Min {
						t.Min = v.Total
					}
					aggregator[name].Component.Metrics[k] = t
				}

			} else {
				// add metrics to component
				for k, v := range serialize(&metric) {
					aggregator[name].Component.Metrics[k] = v
				}
			}
		} else {
			// new component
			var host string
			if metric.HasTag("host") {
				host = metric.Tags()["host"]
				//metric.RemoveTag("host")
			} else {
				host = request.Agent.Host
			}

			var c = NRComponent{
				Name:     host,
				Duration: 60, // TODO find duration
				Metrics:  serialize(&metric),
				GUID:     n.GUID, //+ metric.Name(),
			}
			aggregator[name] = Aggregator{
				Tags:      buildTags(metric.Tags()),
				Component: c,
			}
		}
	}

	var temp []NRComponent
	for _, lookup := range aggregator {
		temp = append(temp, lookup.Component)
	}

	return temp
}

func buildTags(tags map[string]string) string {
	var keys []string
	for k := range tags {
		if k == "host" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var tag_str string
	for i, k := range keys {
		var tag_value string
		if tags[k] == "/" {
			tag_value = "ROOT"
		} else {
			tag_value = sanitizedChars.Replace(tags[k])
		}
		if i == 0 {
			tag_str += /* k + "/" +*/ tag_value
		} else {
			tag_str += /*"/" + k + */ "/" + tag_value
		}
	}
	return tag_str
}

func sendData(n *NewRelic, request NRRequest) error {
	reqbody, err := ejson.Marshal(request)
	if err != nil {
		return fmt.Errorf("unable to marshal request data: %s\n", err.Error())
	}

	req, err := http.NewRequest("POST", n.URL, bytes.NewBuffer(reqbody))
	if err != nil {
		return fmt.Errorf("unable to create http.Request: %s\n", err.Error())
	}
	req.Header.Add("Content-Type", mimetype)
	req.Header.Set("Accept", mimetype)
	req.Header.Set(licence_header, n.License)

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("error POSTing metrics, %s\n", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 209 {
		switch resp.StatusCode {
		case 400, 404, 405:
			return fmt.Errorf("Status %d: maybe update agent", resp.StatusCode)
		case 403:
			return fmt.Errorf("Authentication error (no license key header, or invalid license key).")
		case 413:
			return fmt.Errorf("Request entity too large: Too many metrics were sent in one request")
		case 500, 502, 503, 504:
			return fmt.Errorf("Status %d: Newrelic API not available", resp.StatusCode)
		default:
			return fmt.Errorf("received bad status code: %d\n", resp.StatusCode)
		}

	}

	body, _ := ioutil.ReadAll(resp.Body)

	nrresp := NRResponse{}
	if err := ejson.Unmarshal(body, &nrresp); err != nil {
		return fmt.Errorf("received bad response data: %s %s\n", body, err)
	}

	if nrresp.Error != "" {
		return fmt.Errorf("NewRelic error: %s\n", nrresp.Error)
	}
	if nrresp.Status != "ok" {
		return fmt.Errorf("NewRelic Status not ok: %s\n", nrresp.Status)
	}
	return nil
}

func init() {
	outputs.Add("newrelic", func() telegraf.Output {
		return &NewRelic{}
	})
}
