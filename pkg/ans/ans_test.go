package ans

import (
	"fmt"
	"github.com/SAP/jenkins-library/pkg/xsuaa"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestANS_Send(t *testing.T) {
	testXSUAA := xsuaa.XSUAA{
		OAuthURL:     "https://my.test.oauth.provider",
		ClientID:     "myTestClientID",
		ClientSecret: "super secret",
		CachedAuthToken: xsuaa.AuthToken{
			TokenType:   "bearer",
			AccessToken: "1234",
			ExpiresIn:   12345,
		},
	}
	type request struct {
		path       string
		authHeader string
		event      string
	}
	tests := []struct {
		name        string
		event       Event
		wantErrf    string
		wantRequest request
	}{
		{
			name: "Successfully send event",
			event: Event{
				EventType:      "my event",
				EventTimestamp: 1647526655,
			},
			wantRequest: request{
				path:       "/cf/producer/v1/resource-events",
				authHeader: "bearer 1234",
				event:      `{"eventType":"my event","eventTimestamp":1647526655}`,
			},
		},
		{
			name: "Wrong status code in response error",
			wantErrf: "ANS http request to '%s/cf/producer/v1/resource-events' failed. Did not get expected status code 202; " +
				"instead got 200; response body: an error occurred",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestedUrlPath string
			var requestedMethod string
			var requestedAuthHeader string
			var requestedContentTypeHeader string
			var requestedBody string
			// Start a local HTTP server
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				requestedUrlPath = req.URL.String()
				requestedMethod = req.Method
				if tt.wantErrf == "" {
					rw.WriteHeader(http.StatusAccepted)
				} else {
					rw.Write([]byte("an error occurred"))
				}
				requestedAuthHeader = req.Header.Get(authHeaderKey)
				requestedContentTypeHeader = req.Header.Get("Content-Type")
				requestedBodyBytes, err := ioutil.ReadAll(req.Body)
				require.NoError(t, err)
				requestedBody = string(requestedBodyBytes)
			}))
			ans := ANS{
				XSUAA: testXSUAA,
				URL:   server.URL,
			}
			err := ans.Send(tt.event)
			if len(tt.wantErrf) > 0 {
				require.EqualError(t, err, fmt.Sprintf(tt.wantErrf, server.URL), "An error was expected.")
			} else {
				require.NoError(t, err, "No error expected.")
				assert.Equal(t, tt.wantRequest.path, requestedUrlPath, "Mismatch in requested path")
				assert.Equal(t, http.MethodPost, requestedMethod, "Mismatch in requested method")
				assert.Equal(t, tt.wantRequest.authHeader, requestedAuthHeader, "Mismatch in requested auth header")
				assert.Equal(t, "application/json", requestedContentTypeHeader, "Mismatch in requested content type header")
				assert.Equal(t, tt.wantRequest.event, requestedBody, "Mismatch in requested body")
			}
		})
	}
}

func TestUnmarshallServiceKey(t *testing.T) {
	tests := []struct {
		name              string
		serviceKeyJSON    string
		wantAnsServiceKey ServiceKey
		wantErr           bool
	}{
		{
			name: "Proper event JSON yields correct event",
			serviceKeyJSON: `{
						"url": "https://my.test.backend",
						"client_id": "myTestClientID",
						"client_secret": "super secret",
						"oauth_url": "https://my.test.oauth.provider"
					}`,
			wantAnsServiceKey: ServiceKey{
				Url:          "https://my.test.backend",
				ClientId:     "myTestClientID",
				ClientSecret: "super secret",
				OauthUrl:     "https://my.test.oauth.provider",
			},
			wantErr: false,
		},
		{
			name:           "Faulty JSON yields error",
			serviceKeyJSON: `bli-da-blup`,
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAnsServiceKey, err := UnmarshallServiceKeyJSON(tt.serviceKeyJSON)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshallServiceKeyJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.wantAnsServiceKey, gotAnsServiceKey, "Got the wrong ans serviceKey")
		})
	}
}

func TestTranslateLogrusLogLevel(t *testing.T) {
	tests := []struct {
		name         string
		level        logrus.Level
		wantSeverity string
		wantCategory string
	}{
		{
			name:         "InfoLevel yields INFO and NOTIFICATION",
			level:        logrus.InfoLevel,
			wantSeverity: infoSeverity,
			wantCategory: notificationCategory,
		},
		{
			name:         "DebugLevel yields INFO and NOTIFICATION",
			level:        logrus.DebugLevel,
			wantSeverity: infoSeverity,
			wantCategory: notificationCategory,
		},
		{
			name:         "WarnLevel yields WARNING and ALERT",
			level:        logrus.WarnLevel,
			wantSeverity: warningSeverity,
			wantCategory: alertCategory,
		},
		{
			name:         "ErrorLevel yields ERROR and EXCEPTION",
			level:        logrus.ErrorLevel,
			wantSeverity: errorSeverity,
			wantCategory: exceptionCategory,
		},
		{
			name:         "FatalLevel yields FATAL and EXCEPTION",
			level:        logrus.FatalLevel,
			wantSeverity: fatalSeverity,
			wantCategory: exceptionCategory,
		},
		{
			name:         "PanicLevel yields FATAL and EXCEPTION",
			level:        logrus.PanicLevel,
			wantSeverity: fatalSeverity,
			wantCategory: exceptionCategory,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSeverity, gotCategory := TranslateLogrusLogLevel(tt.level)
			assert.Equal(t, tt.wantSeverity, gotSeverity, "Got wrong severity")
			assert.Equal(t, tt.wantCategory, gotCategory, "Got wrong category")
		})
	}
}

func TestUnmarshallEventJSON(t *testing.T) {
	tests := []struct {
		name          string
		eventJSON     string
		existingEvent Event
		wantEvent     Event
		wantErr       bool
	}{
		{
			name:      "Proper event JSON yields correct event",
			eventJSON: `{"eventType": "my event","eventTimestamp":1647526655}`,
			wantEvent: Event{
				EventType:      "my event",
				EventTimestamp: 1647526655,
			},
			wantErr: false,
		},
		{
			name:      "Merging events includes all parts",
			eventJSON: `{"eventType": "my event", "eventTimestamp": 1647526655, "tags": {"we": "were", "here": "first"}, "resource": {"resourceGroup": "blarp", "resourceName": "was changed"}}`,
			existingEvent: Event{
				EventType: "Bleep",
				Subject:   "Bloop",
				Tags:      map[string]interface{}{"Some": 1.0, "Additional": "a string", "Tags": true},
				Resource: &Resource{
					ResourceType: "blurp",
					ResourceName: "blorp",
				},
			},
			wantEvent: Event{
				EventType:      "my event",
				EventTimestamp: 1647526655,
				Subject:        "Bloop",
				Tags:           map[string]interface{}{"we": "were", "here": "first", "Some": 1.0, "Additional": "a string", "Tags": true},
				Resource: &Resource{
					ResourceType:  "blurp",
					ResourceName:  "was changed",
					ResourceGroup: "blarp",
				},
			},
			wantErr: false,
		},
		{
			name:      "Faulty JSON yields error",
			eventJSON: `bli-da-blup`,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEvent := tt.existingEvent
			err := gotEvent.MergeWithJSON([]byte(tt.eventJSON))
			if (err != nil) != tt.wantErr {
				t.Errorf("MergeWithJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.wantEvent, gotEvent, "Received Event is not as expected.")
		})
	}
}
