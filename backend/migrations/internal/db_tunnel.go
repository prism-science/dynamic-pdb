package internal

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"golang.org/x/exp/maps"
)

func NewDBTunnelCmd() Runner {
	cmd := &dbTunnelCmd{
		tunnelEnvToLocalPort: map[string]defaultTunnelsSettings{
			"dev": {
				localPort: 15432,
				cluster:   "DevECSCluster",
				service:   "ExecAccessService",
				container: "DBAccessContainer",
			},
			"prod": {
				localPort: 25432,
				cluster:   "ProdECSCluster",
				service:   "ExecAccessService",
				container: "DBAccessContainer",
			},
		},
		fs: flag.NewFlagSet("dbTunnel", flag.ExitOnError),
	}

	cmd.fs.StringVar(&cmd.cluster, "cluster", "", "ECS cluster with the service to tunnel to")
	cmd.fs.StringVar(&cmd.service, "service", "", "ECS service to tunnel to")
	cmd.fs.StringVar(&cmd.container, "container", "", "ECS container to tunnel to")

	return cmd
}

type defaultTunnelsSettings struct {
	localPort int
	cluster   string
	service   string
	container string
}

type dbTunnelCmd struct {
	fs *flag.FlagSet

	environment          string
	tunnelEnvToLocalPort map[string]defaultTunnelsSettings

	cluster   string
	service   string
	container string
	localPort int

	// onOutputLine, when non-nil, receives each line read from the SSM
	// session's stdout/stderr instead of slog. Used by tests to observe that
	// every line is captured before the pipes are closed; nil in production.
	onOutputLine func(line string)
}

func (d *dbTunnelCmd) Init(args []string) error {
	if len(args) >= 1 {
		d.environment = args[0]
	}

	if _, exists := d.tunnelEnvToLocalPort[d.environment]; !exists {
		dbTunnelEnvironments := maps.Keys(d.tunnelEnvToLocalPort)
		return fmt.Errorf("invalid environment: %q, one of %+v expected", d.environment, dbTunnelEnvironments)
	}

	err := d.fs.Parse(args)
	if err != nil {
		return fmt.Errorf("parse db-tunnel args: %w", err)
	}
	if d.cluster == "" {
		d.cluster = d.tunnelEnvToLocalPort[d.environment].cluster
	}
	if d.service == "" {
		d.service = d.tunnelEnvToLocalPort[d.environment].service
	}
	if d.container == "" {
		d.container = d.tunnelEnvToLocalPort[d.environment].container
	}
	if d.localPort == 0 {
		d.localPort = d.tunnelEnvToLocalPort[d.environment].localPort
	}
	return nil
}

func (d *dbTunnelCmd) Run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	ecsClient := ecs.NewFromConfig(cfg)
	taskARNs, err := ecsClient.ListTasks(ctx, &ecs.ListTasksInput{
		Cluster:     &d.cluster,
		ServiceName: &d.service,
	})
	if err != nil {
		return fmt.Errorf("list ECS tasks: %w", err)
	}
	tasks, err := ecsClient.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: &d.cluster,
		Tasks:   taskARNs.TaskArns,
	})
	if err != nil {
		return fmt.Errorf("describe ECS tasks: %w", err)
	}

	if len(tasks.Tasks) == 0 {
		return fmt.Errorf("no tasks found, expected task for service: %q in cluster %q", d.service, d.cluster)
	}

	var ecsContainer types.Container
	var task types.Task
	var found bool
	for _, t := range tasks.Tasks {
		for _, container := range t.Containers {
			if *container.Name == d.container {
				ecsContainer = container
				task = t
				found = true
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("no container found, expected container: %q in task for service: %q in cluster %q", d.container, d.service, d.cluster)
	}

	taskParts := strings.Split(*task.TaskArn, "/")
	taskID := taskParts[len(taskParts)-1]
	ecsInstanceID := fmt.Sprintf("ecs:%s_%s_%s", d.cluster, taskID, *ecsContainer.RuntimeId)
	fmt.Println("Found instance ID ", ecsInstanceID)

	// we don't use ssm client from the SDK because anyway it needs a configured aws ssm plugin
	// see https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html
	// and https://docs.aws.amazon.com/systems-manager/latest/userguide/install-plugin-verify.html
	// furthermore, we will have to properly handle the session, like it's done in
	// https://github.com/mmmorris1975/ssm-session-client
	// it means there is no point in using the SDK for this task
	cmd := exec.Command(
		"aws", "ssm", "start-session",
		"--target", ecsInstanceID,
		"--document-name", "AWS-StartPortForwardingSession",
		"--parameters", fmt.Sprintf(`{"portNumber":["5432"], "localPortNumber":["%d"]}`, d.localPort),
	)
	return d.streamCommand(cmd)
}

// streamCommand starts cmd, drains its stdout/stderr through asyncOutPipe, and
// only calls cmd.Wait once both reader goroutines have finished. os/exec
// documents that Wait closes the StdoutPipe/StderrPipe descriptors and that
// "it is incorrect to call Wait before all reads from the pipe have completed."
// The previous code used cmd.Run (= Start + Wait), so Wait closed the read ends
// while the scanners were still reading — trailing output was lost, the reader
// goroutines were never joined, and the read-vs-close was a data race. Joining
// the goroutines (wg.Wait) before cmd.Wait removes all three.
func (d *dbTunnelCmd) streamCommand(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start SSM session: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d.asyncOutPipe(stdout)
	}()
	go func() {
		defer wg.Done()
		d.asyncOutPipe(stderr)
	}()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("run SSM session: %w", err)
	}
	return nil
}

func (d *dbTunnelCmd) Name() string {
	return "db-tunnel"
}

func (d *dbTunnelCmd) asyncOutPipe(pipe io.ReadCloser) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		if d.onOutputLine != nil {
			d.onOutputLine(line)
			continue
		}
		slog.Info("db-tunnel: aws ssm output", "line", line)
	}
}
