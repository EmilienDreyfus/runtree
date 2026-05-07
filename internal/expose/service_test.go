package expose

import (
	"context"
	"errors"
	"testing"

	"github.com/EmilienDreyfus/runtree/internal/app"
	"github.com/EmilienDreyfus/runtree/internal/cloudapi"
	"github.com/EmilienDreyfus/runtree/internal/config"
	"github.com/EmilienDreyfus/runtree/internal/state"
)

type fakeAppService struct {
	project app.ProjectContext
	running bool

	startCalls int
	stopCalls  int
}

func (f *fakeAppService) InstanceDetails(string, string) (app.ProjectContext, state.Instance, error) {
	status := state.StatusStopped
	if f.running {
		status = state.StatusRunning
	}
	return f.project, state.Instance{
		Name:   "main",
		Branch: "main",
		Port:   8100,
		Status: status,
	}, nil
}

func (f *fakeAppService) StartInstance(string, string) (state.Instance, error) {
	f.running = true
	f.startCalls++
	return state.Instance{Name: "main", Port: 8100, Status: state.StatusRunning}, nil
}

func (f *fakeAppService) StopInstance(string, string) (state.Instance, error) {
	f.running = false
	f.stopCalls++
	return state.Instance{Name: "main", Port: 8100, Status: state.StatusStopped}, nil
}

type fakeCloudClient struct {
	createResp     cloudapi.CreateExposureResponse
	createErr      error
	heartbeatCalls int
	teardownCalls  int
	lastCreateReq  cloudapi.CreateExposureRequest
}

func (f *fakeCloudClient) CreateExposure(_ context.Context, req cloudapi.CreateExposureRequest) (cloudapi.CreateExposureResponse, error) {
	f.lastCreateReq = req
	return f.createResp, f.createErr
}

func (f *fakeCloudClient) HeartbeatExposure(context.Context, string) error {
	f.heartbeatCalls++
	return nil
}

func (f *fakeCloudClient) TeardownExposure(context.Context, string) error {
	f.teardownCalls++
	return nil
}

type fakeTunnelRunner struct {
	runErr     error
	runCalls   int
	lastLaunch cloudapi.TunnelLaunchConfig
}

func (f *fakeTunnelRunner) Run(_ context.Context, launch cloudapi.TunnelLaunchConfig) error {
	f.runCalls++
	f.lastLaunch = launch
	return f.runErr
}

func TestRunAutoStartsAndStopsInstance(t *testing.T) {
	t.Parallel()

	appService := &fakeAppService{
		project: app.ProjectContext{
			Config:  &config.Config{Web: config.WebConfig{URLTemplate: "http://127.0.0.1:{port}"}},
			Project: state.Project{Name: "demo"},
		},
	}
	cloudClient := &fakeCloudClient{
		createResp: cloudapi.CreateExposureResponse{
			ExposureID: "exp_123",
			PublicURL:  "https://demo.tunnel.runtree.dev",
			Launch: cloudapi.TunnelLaunchConfig{
				Provider:        "cloudflare",
				Kind:            "cloudflare_named_tunnel",
				ConfigTemplate:  "tunnel: demo",
				CredentialsJSON: "{}",
			},
		},
	}
	runner := &fakeTunnelRunner{}

	service := Service{App: appService, Cloud: cloudClient, Runner: runner}
	if err := service.Run(context.Background(), ".", "main"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if appService.startCalls != 1 {
		t.Fatalf("startCalls = %d, want 1", appService.startCalls)
	}
	if appService.stopCalls != 1 {
		t.Fatalf("stopCalls = %d, want 1", appService.stopCalls)
	}
	if cloudClient.teardownCalls != 1 {
		t.Fatalf("teardownCalls = %d, want 1", cloudClient.teardownCalls)
	}
	if runner.runCalls != 1 {
		t.Fatalf("runCalls = %d, want 1", runner.runCalls)
	}
}

func TestRunLeavesExistingInstanceRunning(t *testing.T) {
	t.Parallel()

	appService := &fakeAppService{
		running: true,
		project: app.ProjectContext{
			Config:  &config.Config{Web: config.WebConfig{URLTemplate: "http://127.0.0.1:{port}"}},
			Project: state.Project{Name: "demo"},
		},
	}
	cloudClient := &fakeCloudClient{
		createResp: cloudapi.CreateExposureResponse{
			ExposureID: "exp_123",
			PublicURL:  "https://demo.tunnel.runtree.dev",
			Launch: cloudapi.TunnelLaunchConfig{
				Provider:        "cloudflare",
				Kind:            "cloudflare_named_tunnel",
				ConfigTemplate:  "tunnel: demo",
				CredentialsJSON: "{}",
			},
		},
	}

	service := Service{App: appService, Cloud: cloudClient, Runner: &fakeTunnelRunner{}}
	if err := service.Run(context.Background(), ".", "main"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if appService.startCalls != 0 {
		t.Fatalf("startCalls = %d, want 0", appService.startCalls)
	}
	if appService.stopCalls != 0 {
		t.Fatalf("stopCalls = %d, want 0", appService.stopCalls)
	}
}

func TestRunStopsAutoStartedInstanceOnCreateFailure(t *testing.T) {
	t.Parallel()

	appService := &fakeAppService{
		project: app.ProjectContext{
			Config:  &config.Config{Web: config.WebConfig{URLTemplate: "http://127.0.0.1:{port}"}},
			Project: state.Project{Name: "demo"},
		},
	}
	cloudClient := &fakeCloudClient{createErr: errors.New("upgrade required")}

	service := Service{App: appService, Cloud: cloudClient, Runner: &fakeTunnelRunner{}}
	err := service.Run(context.Background(), ".", "main")
	if err == nil {
		t.Fatal("Run() error = nil, want create exposure error")
	}
	if appService.stopCalls != 1 {
		t.Fatalf("stopCalls = %d, want 1", appService.stopCalls)
	}
}
