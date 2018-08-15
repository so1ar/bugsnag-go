package sessions

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bugsnag/bugsnag-go/sessions/testutil"
	uuid "github.com/satori/go.uuid"
)

var receivedRequests chan testutil.SessionRequest

func TestMain(m *testing.M) {
	receivedRequests = testutil.StartSessionTestServer(sessionAuthority)
	defer close(receivedRequests)
	retCode := m.Run()
	os.Exit(retCode)
}

const sessionAuthority string = "localhost:9182"
const testAPIKey = "166f5ad3590596f9aa8d601ea89af845"

func TestSendsCorrectPayloadForSmallConfig(t *testing.T) {
	sessions, earliestTime := makeSessions()
	config := SessionTrackingConfiguration{
		Endpoint:  "http://" + sessionAuthority,
		Transport: http.DefaultTransport,
		APIKey:    testAPIKey,
	}
	(&defaultPublisher{config: config}).publish(sessions)
	req := <-receivedRequests
	assertCorrectHeaders(t, req)
	root := extractPayload(t, req)
	hostname, _ := os.Hostname()
	testCases := []struct {
		property string
		expected string
	}{
		{property: "notifier.name", expected: "Bugsnag Go"},
		{property: "notifier.url", expected: "https://github.com/bugsnag/bugsnag-go"},
		{property: "notifier.version", expected: ""},
		{property: "app.type", expected: ""},
		{property: "app.releaseStage", expected: "production"},
		{property: "app.version", expected: ""},
		{property: "device.osName", expected: runtime.GOOS},
		{property: "device.hostname", expected: hostname},
		{property: "sessionCounts.startedAt", expected: earliestTime},
	}
	for _, tc := range testCases {
		t.Run(tc.property, func(st *testing.T) {
			got, err := getJSONString(root, tc.property)
			if err != nil {
				t.Error(err)
			}
			if got != tc.expected {
				t.Errorf("Expected property '%s' in JSON to be '%s' but was '%s'", tc.property, tc.expected, got)
			}
		})
	}
	assertSessionsStarted(t, root, len(sessions))
}

func TestSendsCorrectPayloadForBigConfig(t *testing.T) {
	sessions, earliestTime := makeSessions()
	(&defaultPublisher{config: makeHeavilyConfiguredConfig()}).publish(sessions)
	req := <-receivedRequests
	assertCorrectHeaders(t, req)
	root := extractPayload(t, req)
	testCases := []struct {
		property string
		expected string
	}{
		{property: "notifier.name", expected: "Bugsnag Go"},
		{property: "notifier.url", expected: "https://github.com/bugsnag/bugsnag-go"},
		{property: "notifier.version", expected: "2.3.4-alpha"},
		{property: "app.type", expected: "gin"},
		{property: "app.releaseStage", expected: "staging"},
		{property: "app.version", expected: "1.2.3-beta"},
		{property: "device.osName", expected: runtime.GOOS},
		{property: "device.hostname", expected: "gce-1234-us-west-1"},
		{property: "sessionCounts.startedAt", expected: earliestTime},
	}
	for _, tc := range testCases {
		t.Run(tc.property, func(st *testing.T) {
			got, err := getJSONString(root, tc.property)
			if err != nil {
				t.Error(err)
			}
			if got != tc.expected {
				t.Errorf("Expected property '%s' in JSON to be '%s' but was '%s'", tc.property, tc.expected, got)
			}
		})
	}
	assertSessionsStarted(t, root, len(sessions))
}

func getJSONString(root *json.RawMessage, path string) (string, error) {
	if strings.Contains(path, ".") {
		split := strings.Split(path, ".")
		subobj, err := getNestedJSON(root, split[0])
		if err != nil {
			return "", err
		}
		return getJSONString(subobj, strings.Join(split[1:], "."))
	}
	var m map[string]json.RawMessage
	err := json.Unmarshal(*root, &m)
	if err != nil {
		return "", err
	}
	var s string
	err = json.Unmarshal(m[path], &s)
	if err != nil {
		return "", err
	}
	return s, nil
}

func getNestedJSON(root *json.RawMessage, path string) (*json.RawMessage, error) {
	var subobj map[string]*json.RawMessage
	err := json.Unmarshal(*root, &subobj)
	if err != nil {
		return nil, err
	}
	return subobj[path], nil
}

func makeHeavilyConfiguredConfig() SessionTrackingConfiguration {
	return SessionTrackingConfiguration{
		AppType:      "gin",
		APIKey:       testAPIKey,
		AppVersion:   "1.2.3-beta",
		Version:      "2.3.4-alpha",
		Endpoint:     "http://" + sessionAuthority,
		Transport:    http.DefaultTransport,
		ReleaseStage: "staging",
		Hostname:     "gce-1234-us-west-1",
	}
}

func makeSessions() ([]session, string) {
	earliestTime := time.Now()
	return []session{
		{startedAt: earliestTime, id: uuid.NewV4()},
		{startedAt: earliestTime.Add(2 * time.Minute), id: uuid.NewV4()},
		{startedAt: earliestTime.Add(4 * time.Minute), id: uuid.NewV4()},
	}, earliestTime.UTC().Format(time.RFC3339)
}

func extractPayload(t *testing.T, req testutil.SessionRequest) *json.RawMessage {
	var root json.RawMessage
	err := json.Unmarshal(req.Body, &root)
	if err != nil {
		t.Fatal(err)
	}
	return &root
}

func assertSessionsStarted(t *testing.T, root *json.RawMessage, expected int) {
	subobj, err := getNestedJSON(root, "sessionCounts")
	if err != nil {
		t.Fatal(err)
	}
	var sessionCounts map[string]*json.RawMessage
	err = json.Unmarshal(*subobj, &sessionCounts)
	if err != nil {
		t.Fatal(err)
	}
	var got int
	err = json.Unmarshal(*sessionCounts["sessionsStarted"], &got)
	if got != expected {
		t.Errorf("Expected %d sessions to be registered but was %d", expected, got)
	}
}

func assertCorrectHeaders(t *testing.T, req testutil.SessionRequest) {
	testCases := []struct{ name, expected string }{
		{name: "Bugsnag-Payload-Version", expected: "1"},
		{name: "Content-Type", expected: "application/json"},
		{name: "Bugsnag-Api-Key", expected: testAPIKey},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(st *testing.T) {
			if got := req.Header[tc.name][0]; tc.expected != got {
				t.Errorf("Expected header '%s' to be '%s' but was '%s'", tc.name, tc.expected, got)
			}
		})
	}
	name := "Bugsnag-Sent-At"
	if req.Header[name][0] == "" {
		t.Errorf("Expected header '%s' to be non-empty but was empty", name)
	}
}
