package expose

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/EmilienDreyfus/runtree/internal/app"
	"github.com/EmilienDreyfus/runtree/internal/cloudapi"
	"github.com/EmilienDreyfus/runtree/internal/state"
	"github.com/EmilienDreyfus/runtree/internal/termui"
)

type AppService interface {
	InstanceDetails(startDir, name string) (app.ProjectContext, state.Instance, error)
	StartInstance(startDir, name string) (state.Instance, error)
	StopInstance(startDir, name string) (state.Instance, error)
}

type CloudClient interface {
	CreateExposure(ctx context.Context, req cloudapi.CreateExposureRequest) (cloudapi.CreateExposureResponse, error)
	HeartbeatExposure(ctx context.Context, exposureID string) error
	TeardownExposure(ctx context.Context, exposureID string) error
}

type TunnelRunner interface {
	Run(ctx context.Context, launch cloudapi.TunnelLaunchConfig) error
}

type RunState struct {
	ExposureID  string
	PublicURL   string
	AutoStarted bool
}

type Service struct {
	App      AppService
	Cloud    CloudClient
	Runner   TunnelRunner
	Log      io.Writer
	OnReady  func(RunState)
	Progress termui.Reporter
}

func (s Service) Run(ctx context.Context, startDir, instanceName string) error {
	if s.App == nil {
		return errors.New("app service is required")
	}
	if s.Cloud == nil {
		return errors.New("cloud client is required")
	}
	if s.Runner == nil {
		return errors.New("tunnel runner is required")
	}

	projectCtx, instance, err := s.App.InstanceDetails(startDir, instanceName)
	if err != nil {
		return err
	}
	if instance.Status == state.StatusMissing {
		return fmt.Errorf("instance %s is missing its worktree", instance.Name)
	}

	autoStarted := false
	if instance.Status != state.StatusRunning {
		startStep := startProgress(s.Progress, fmt.Sprintf("Starting %s", instance.Name))
		instance, err = s.App.StartInstance(startDir, instanceName)
		if err != nil {
			failProgress(startStep, "start failed")
			return err
		}
		successProgress(startStep, fmt.Sprintf("started %s", instance.Name))
		autoStarted = true
	}

	stopAutoStarted := func() error {
		if !autoStarted {
			return nil
		}
		_, stopErr := s.App.StopInstance(startDir, instanceName)
		return stopErr
	}

	projectCtx, instance, err = s.App.InstanceDetails(startDir, instanceName)
	if err != nil {
		_ = stopAutoStarted()
		return err
	}

	createStep := startProgress(s.Progress, "Creating public URL")
	createResp, err := s.Cloud.CreateExposure(ctx, cloudapi.CreateExposureRequest{
		ProjectName:  projectCtx.Project.Name,
		InstanceName: instance.Name,
		Branch:       instance.Branch,
		LocalURL:     projectCtx.Config.RenderWebURL(instance.Port),
		LocalPort:    instance.Port,
	})
	if err != nil {
		failProgress(createStep, "public URL failed")
		_ = stopAutoStarted()
		return err
	}

	runState := RunState{
		ExposureID:  createResp.ExposureID,
		PublicURL:   createResp.PublicURL,
		AutoStarted: autoStarted,
	}
	successProgress(createStep, "public URL ready")
	if s.OnReady != nil {
		s.OnReady(runState)
	}

	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		s.runHeartbeatLoop(heartbeatCtx, createResp.HeartbeatIntervalSeconds, createResp.ExposureID)
	}()

	runErr := s.Runner.Run(ctx, createResp.Launch)
	cancelHeartbeat()
	<-heartbeatDone

	teardownErr := s.Cloud.TeardownExposure(context.Background(), createResp.ExposureID)
	stopErr := stopAutoStarted()

	if errors.Is(runErr, context.Canceled) {
		runErr = nil
	}
	return errors.Join(runErr, teardownErr, stopErr)
}

func startProgress(progress termui.Reporter, message string) termui.Step {
	if progress == nil {
		return nil
	}
	return progress.Start(message)
}

func successProgress(step termui.Step, message string) {
	if step != nil {
		step.Success(message)
	}
}

func failProgress(step termui.Step, message string) {
	if step != nil {
		step.Fail(message)
	}
}

func (s Service) runHeartbeatLoop(ctx context.Context, intervalSeconds int, exposureID string) {
	if intervalSeconds <= 0 {
		intervalSeconds = 15
	}

	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Cloud.HeartbeatExposure(ctx, exposureID); err != nil && s.Log != nil {
				fmt.Fprintf(s.Log, "warning: heartbeat failed: %v\n", err)
			}
		}
	}
}
