package authflow

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/EmilienDreyfus/runtree/internal/authstore"
	"github.com/EmilienDreyfus/runtree/internal/cloudapi"
)

type fakeDeviceLoginClient struct {
	startResp cloudapi.DeviceLoginStartResponse
	polls     []cloudapi.DeviceLoginPollResponse
}

func (f *fakeDeviceLoginClient) StartDeviceLogin(context.Context) (cloudapi.DeviceLoginStartResponse, error) {
	return f.startResp, nil
}

func (f *fakeDeviceLoginClient) PollDeviceLogin(context.Context, string) (cloudapi.DeviceLoginPollResponse, error) {
	if len(f.polls) == 0 {
		return cloudapi.DeviceLoginPollResponse{Status: "pending"}, nil
	}
	resp := f.polls[0]
	f.polls = f.polls[1:]
	return resp, nil
}

func TestLoginSavesApprovedSession(t *testing.T) {
	t.Parallel()

	client := &fakeDeviceLoginClient{
		startResp: cloudapi.DeviceLoginStartResponse{
			DeviceCode:          "device-123",
			VerificationURL:     "https://runtree.dev/device",
			ExpiresAt:           time.Now().Add(5 * time.Minute),
			PollIntervalSeconds: 0,
		},
		polls: []cloudapi.DeviceLoginPollResponse{
			{Status: "pending"},
			{Status: "approved", AccessToken: "token-123", AccountHandle: "emilien", BaseURL: "https://runtree.dev"},
		},
	}

	home := t.TempDir()
	var out bytes.Buffer
	service := Service{
		HomeDir: home,
		Client:  client,
		OpenBrowser: func(string) error {
			return errors.New("boom")
		},
	}

	auth, err := service.Login(context.Background(), &out)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if auth.AccountHandle != "emilien" {
		t.Fatalf("Login().AccountHandle = %q", auth.AccountHandle)
	}

	loaded, err := authstore.Load(home)
	if err != nil {
		t.Fatalf("authstore.Load() error = %v", err)
	}
	if loaded.AccessToken != "token-123" {
		t.Fatalf("loaded token = %q", loaded.AccessToken)
	}
}

func TestLogoutClearsSavedAuth(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := authstore.Save(home, authstore.Auth{
		BaseURL:       "https://runtree.dev",
		AccessToken:   "token-123",
		AccountHandle: "emilien",
	}); err != nil {
		t.Fatalf("authstore.Save() error = %v", err)
	}

	service := Service{HomeDir: home}
	if err := service.Logout(); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	loaded, err := authstore.Load(home)
	if err != nil {
		t.Fatalf("authstore.Load() error = %v", err)
	}
	if loaded != (authstore.Auth{}) {
		t.Fatalf("auth after logout = %+v, want zero auth", loaded)
	}
}
