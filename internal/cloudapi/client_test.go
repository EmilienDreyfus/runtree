package cloudapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCreateExposureUsesBearerToken(t *testing.T) {
	t.Parallel()

	client := NewClient("https://runtree.test", "token-123")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("Authorization header = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"exposure_id":"exp_123",
			"public_url":"https://demo.tunnel.runtree.dev",
			"heartbeat_interval_seconds":15,
			"launch":{
				"provider":"cloudflare",
				"kind":"cloudflare_named_tunnel",
				"config_template":"tunnel: demo",
				"credentials_json":"{}"
			}
		}`)),
		}, nil
	})}
	resp, err := client.CreateExposure(context.Background(), CreateExposureRequest{
		ProjectName:  "demo",
		InstanceName: "main",
		LocalURL:     "http://127.0.0.1:8100",
		LocalPort:    8100,
	})
	if err != nil {
		t.Fatalf("CreateExposure() error = %v", err)
	}
	if resp.PublicURL != "https://demo.tunnel.runtree.dev" {
		t.Fatalf("CreateExposure().PublicURL = %q", resp.PublicURL)
	}
}

func TestAPIErrorIsDecoded(t *testing.T) {
	t.Parallel()

	client := NewClient("https://runtree.test", "")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusPaymentRequired,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"error":{
				"code":"upgrade_required",
				"message":"upgrade required",
				"upgrade_url":"https://runtree.dev/pricing"
			}
		}`)),
		}, nil
	})}
	_, err := client.CreateExposure(context.Background(), CreateExposureRequest{})
	if err == nil {
		t.Fatal("CreateExposure() error = nil, want APIError")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Code != "upgrade_required" || apiErr.UpgradeURL == "" {
		t.Fatalf("APIError = %+v", apiErr)
	}
}
