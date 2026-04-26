package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerExecutor runs jobs as Docker containers.
type DockerExecutor struct {
	client *client.Client
}

// NewDockerExecutor creates a DockerExecutor using environment-configured Docker settings.
func NewDockerExecutor() (*DockerExecutor, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return &DockerExecutor{client: cli}, nil
}

// Ping verifies the Docker daemon is reachable.
func (d *DockerExecutor) Ping(ctx context.Context) error {
	_, err := d.client.Ping(ctx)
	return err
}

// Run executes a container with the given options and returns the result.
// The container is removed after execution regardless of outcome.
func (d *DockerExecutor) Run(ctx context.Context, opts RunOptions) RunResult {
	if err := d.ensureImage(ctx, opts.Image); err != nil {
		return RunResult{Error: fmt.Errorf("ensure image %q: %w", opts.Image, err)}
	}

	nanoCPUs, err := parseCPU(opts.CPULimit)
	if err != nil {
		return RunResult{Error: fmt.Errorf("parse cpu limit: %w", err)}
	}
	mem, err := parseMemory(opts.MemoryLimit)
	if err != nil {
		return RunResult{Error: fmt.Errorf("parse memory limit: %w", err)}
	}

	resp, err := d.client.ContainerCreate(
		ctx,
		&container.Config{
			Image: opts.Image,
			Cmd:   opts.Command,
		},
		&container.HostConfig{
			Binds: opts.Volumes,
			Resources: container.Resources{
				NanoCPUs: nanoCPUs,
				Memory:   mem,
			},
		},
		nil, // networking config
		nil, // platform
		"",  // name (auto-generated)
	)
	if err != nil {
		return RunResult{Error: fmt.Errorf("create container: %w", err)}
	}
	containerID := resp.ID

	// Always remove the container when done.
	defer func() {
		bgCtx := context.Background()
		d.client.ContainerRemove(bgCtx, containerID, container.RemoveOptions{Force: true})
	}()

	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return RunResult{
			Error:       fmt.Errorf("start container: %w", err),
			ContainerID: containerID,
		}
	}

	statusCh, errCh := d.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

	select {
	case status := <-statusCh:
		logs := d.collectLogs(containerID)
		var runErr error
		if status.Error != nil {
			runErr = fmt.Errorf("container error: %s", status.Error.Message)
		}
		return RunResult{
			ExitCode:    int(status.StatusCode),
			LogOutput:   logs,
			ContainerID: containerID,
			Error:       runErr,
		}

	case err := <-errCh:
		logs := d.collectLogs(containerID)
		return RunResult{
			Error:       fmt.Errorf("wait for container: %w", err),
			ContainerID: containerID,
			LogOutput:   logs,
			ExitCode:    -1,
		}

	case <-ctx.Done():
		// Timeout or cancellation — kill the container, then collect partial logs.
		bgCtx := context.Background()
		_ = d.client.ContainerKill(bgCtx, containerID, "SIGKILL")
		logs := d.collectLogs(containerID)
		return RunResult{
			Error:       ctx.Err(),
			ContainerID: containerID,
			LogOutput:   logs,
			ExitCode:    -1,
		}
	}
}

func (d *DockerExecutor) ensureImage(ctx context.Context, img string) error {
	_, _, err := d.client.ImageInspectWithRaw(ctx, img)
	if err == nil {
		return nil // already present
	}
	if !client.IsErrNotFound(err) {
		return fmt.Errorf("inspect image: %w", err)
	}
	reader, err := d.client.ImagePull(ctx, img, dockerimage.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	defer reader.Close()
	// Drain to completion so the pull finishes before we proceed.
	_, err = io.Copy(io.Discard, reader)
	return err
}

func (d *DockerExecutor) collectLogs(containerID string) string {
	ctx := context.Background()
	reader, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return ""
	}
	defer reader.Close()

	var buf bytes.Buffer
	// stdcopy demuxes Docker's multiplexed log stream.
	_, _ = stdcopy.StdCopy(&buf, &buf, reader)
	return buf.String()
}

// parseCPU converts a CPU fraction string (e.g. "0.5") to NanoCPUs.
func parseCPU(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cpu limit %q: %w", s, err)
	}
	return int64(f * 1e9), nil
}

// parseMemory converts a memory string (e.g. "128m", "1g") to bytes.
func parseMemory(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	s = strings.ToLower(strings.TrimSpace(s))
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"g", 1024 * 1024 * 1024},
		{"m", 1024 * 1024},
		{"k", 1024},
		{"b", 1},
	}
	for _, sf := range suffixes {
		if strings.HasSuffix(s, sf.suffix) {
			n, err := strconv.ParseInt(s[:len(s)-len(sf.suffix)], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid memory limit %q: %w", s, err)
			}
			return n * sf.mult, nil
		}
	}
	return strconv.ParseInt(s, 10, 64)
}
