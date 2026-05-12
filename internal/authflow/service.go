package authflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/EmilienDreyfus/runtree/internal/authstore"
	"github.com/EmilienDreyfus/runtree/internal/cloudapi"
	"github.com/EmilienDreyfus/runtree/internal/termui"
)

type DeviceLoginClient interface {
	StartDeviceLogin(ctx context.Context) (cloudapi.DeviceLoginStartResponse, error)
	PollDeviceLogin(ctx context.Context, deviceCode string) (cloudapi.DeviceLoginPollResponse, error)
}

type Service struct {
	HomeDir     string
	BaseURL     string
	Client      DeviceLoginClient
	OpenBrowser func(string) error
	SaveAuth    func(string, authstore.Auth) error
	ClearAuth   func(string) error
	Progress    termui.Reporter
}

func (s Service) Login(ctx context.Context, out io.Writer) (authstore.Auth, error) {
	if s.Client == nil {
		return authstore.Auth{}, errors.New("device login client is required")
	}
	saveAuth := s.SaveAuth
	if saveAuth == nil {
		saveAuth = authstore.Save
	}

	startResp, err := s.Client.StartDeviceLogin(ctx)
	if err != nil {
		return authstore.Auth{}, err
	}

	if out != nil {
		fmt.Fprintf(out, "finish sign-in in your browser:\n%s\n", startResp.VerificationURL)
	}
	if s.OpenBrowser != nil {
		if err := s.OpenBrowser(startResp.VerificationURL); err != nil && out != nil {
			fmt.Fprintf(out, "could not open browser automatically: %v\n", err)
		}
	}

	var waitStep termui.Step
	if s.Progress != nil {
		waitStep = s.Progress.Start("Waiting for browser approval")
	}

	pollInterval := time.Duration(startResp.PollIntervalSeconds) * time.Second
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	deadline := startResp.ExpiresAt
	if deadline.IsZero() {
		deadline = time.Now().Add(5 * time.Minute)
	}

	for {
		if err := ctx.Err(); err != nil {
			failProgress(waitStep, "login cancelled")
			return authstore.Auth{}, err
		}

		pollResp, err := s.Client.PollDeviceLogin(ctx, startResp.DeviceCode)
		if err != nil {
			failProgress(waitStep, "login failed")
			return authstore.Auth{}, err
		}

		switch strings.ToLower(strings.TrimSpace(pollResp.Status)) {
		case "approved":
			auth := authstore.Auth{
				BaseURL:       firstNonEmpty(pollResp.BaseURL, s.BaseURL, cloudapi.DefaultBaseURL),
				AccessToken:   pollResp.AccessToken,
				AccountHandle: pollResp.AccountHandle,
			}
			if err := saveAuth(s.HomeDir, auth); err != nil {
				failProgress(waitStep, "login failed")
				return authstore.Auth{}, err
			}
			if waitStep != nil {
				waitStep.Success(fmt.Sprintf("logged in as %s", auth.AccountHandle))
			} else if out != nil {
				fmt.Fprintf(out, "logged in as %s\n", auth.AccountHandle)
			}
			return auth, nil
		case "denied":
			failProgress(waitStep, "login denied")
			return authstore.Auth{}, errors.New("login request was denied")
		case "expired":
			failProgress(waitStep, "login expired")
			return authstore.Auth{}, errors.New("login request expired")
		}

		if time.Now().After(deadline) {
			failProgress(waitStep, "login expired")
			return authstore.Auth{}, errors.New("login request expired")
		}

		select {
		case <-ctx.Done():
			failProgress(waitStep, "login cancelled")
			return authstore.Auth{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (s Service) Logout() error {
	clearAuth := s.ClearAuth
	if clearAuth == nil {
		clearAuth = authstore.Clear
	}
	return clearAuth(s.HomeDir)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func failProgress(step termui.Step, message string) {
	if step != nil {
		step.Fail(message)
	}
}
