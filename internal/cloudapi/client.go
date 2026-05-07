package cloudapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL     string
	AccessToken string
	HTTPClient  *http.Client
}

func NewClient(baseURL, accessToken string) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		AccessToken: strings.TrimSpace(accessToken),
		HTTPClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) StartDeviceLogin(ctx context.Context) (DeviceLoginStartResponse, error) {
	var resp DeviceLoginStartResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/cli/login/start/", struct{}{}, &resp)
	return resp, err
}

func (c *Client) PollDeviceLogin(ctx context.Context, deviceCode string) (DeviceLoginPollResponse, error) {
	var resp DeviceLoginPollResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/cli/login/poll/", map[string]string{
		"device_code": deviceCode,
	}, &resp)
	return resp, err
}

func (c *Client) Me(ctx context.Context) (MeResponse, error) {
	var resp MeResponse
	err := c.doJSON(ctx, http.MethodGet, "/api/cli/me/", nil, &resp)
	return resp, err
}

func (c *Client) CreateExposure(ctx context.Context, req CreateExposureRequest) (CreateExposureResponse, error) {
	var resp CreateExposureResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/cli/exposures/", req, &resp)
	return resp, err
}

func (c *Client) HeartbeatExposure(ctx context.Context, exposureID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/cli/exposures/"+url.PathEscape(exposureID)+"/heartbeat/", struct{}{}, nil)
}

func (c *Client) TeardownExposure(ctx context.Context, exposureID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/cli/exposures/"+url.PathEscape(exposureID)+"/teardown/", struct{}{}, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	var body io.Reader
	if requestBody != nil {
		data, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		var envelope apiErrorEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err == nil && envelope.Error.Message != "" {
			envelope.Error.StatusCode = resp.StatusCode
			return &envelope.Error
		}

		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		apiErr.Message = strings.TrimSpace(string(payload))
		if apiErr.Message == "" {
			apiErr.Message = resp.Status
		}
		return apiErr
	}

	if responseBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
