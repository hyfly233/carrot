package containermanager

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"carrot/internal/common"

	"go.uber.org/zap"
)

// ContainerExecutor 容器执行器接口
type ContainerExecutor interface {
	// StartContainer 启动容器
	StartContainer(container *common.Container, launchContext *common.ContainerLaunchContext) (ContainerProcess, error)

	// CleanupContainer 清理容器
	CleanupContainer(containerID common.ContainerID) error

	// GetContainerStatus 获取容器状态
	GetContainerStatus(containerID common.ContainerID) (*ContainerStatus, error)

	// SetResourceLimits 设置资源限制
	SetResourceLimits(containerID common.ContainerID, limits *ResourceLimits) error
}

// DefaultContainerExecutor 默认容器执行器实现
type DefaultContainerExecutor struct {
	mu         sync.RWMutex
	processes  map[string]*DefaultContainerProcess
	workingDir string
	logger     *zap.Logger

	// 配置
	config *ContainerExecutorConfig
}

// ContainerExecutorConfig 容器执行器配置
type ContainerExecutorConfig struct {
	WorkingDirectory string `json:"working_directory"`
	LogDirectory     string `json:"log_directory"`
	TempDirectory    string `json:"temp_directory"`
	EnableCgroups    bool   `json:"enable_cgroups"`
	CgroupsPath      string `json:"cgroups_path"`
	EnableNamespaces bool   `json:"enable_namespaces"`
	DefaultShell     string `json:"default_shell"`
}

// DefaultContainerProcess 默认容器进程实现
type DefaultContainerProcess struct {
	mu          sync.RWMutex
	cmd         *exec.Cmd
	containerID string
	pid         int
	exitCode    int
	isRunning   bool
	startTime   time.Time
	endTime     time.Time
	logger      *zap.Logger

	// 输出文件
	stdoutFile *os.File
	stderrFile *os.File

	// 进程控制
	ctx    context.Context
	cancel context.CancelFunc
}

// ContainerStatus 容器状态
type ContainerStatus struct {
	ContainerID   string         `json:"container_id"`
	PID           int            `json:"pid"`
	State         string         `json:"state"`
	StartTime     time.Time      `json:"start_time"`
	ResourceUsage *ResourceUsage `json:"resource_usage"`
	ExitCode      int            `json:"exit_code"`
	Diagnostics   string         `json:"diagnostics"`
}

// ResourceLimits 资源限制
type ResourceLimits struct {
	Memory    int64 `json:"memory"`     // 内存限制 (bytes)
	CPUShares int64 `json:"cpu_shares"` // CPU 份额
	CPUQuota  int64 `json:"cpu_quota"`  // CPU 配额
	CPUPeriod int64 `json:"cpu_period"` // CPU 周期
}

// NewDefaultContainerExecutor 创建默认容器执行器
func NewDefaultContainerExecutor(config *ContainerExecutorConfig) *DefaultContainerExecutor {
	if config == nil {
		config = &ContainerExecutorConfig{
			WorkingDirectory: "/tmp/carrot/containers",
			LogDirectory:     "/tmp/carrot/logs",
			TempDirectory:    "/tmp/carrot/temp",
			EnableCgroups:    false,
			CgroupsPath:      "/sys/fs/cgroup",
			EnableNamespaces: false,
			DefaultShell:     "/bin/bash",
		}
	}

	// 确保目录存在
	os.MkdirAll(config.WorkingDirectory, 0755)
	os.MkdirAll(config.LogDirectory, 0755)
	os.MkdirAll(config.TempDirectory, 0755)

	return &DefaultContainerExecutor{
		processes:  make(map[string]*DefaultContainerProcess),
		workingDir: config.WorkingDirectory,
		logger:     common.ComponentLogger("container-executor"),
		config:     config,
	}
}

// StartContainer 启动容器
func (dce *DefaultContainerExecutor) StartContainer(container *common.Container, launchContext *common.ContainerLaunchContext) (ContainerProcess, error) {
	containerKey := dce.getContainerKey(container.ID)

	dce.logger.Info("Starting container",
		zap.String("container_id", containerKey),
		zap.Any("resource", container.Resource))

	// 检查容器是否已存在
	dce.mu.Lock()
	if _, exists := dce.processes[containerKey]; exists {
		dce.mu.Unlock()
		return nil, fmt.Errorf("container %s already exists", containerKey)
	}
	dce.mu.Unlock()

	// 创建容器工作目录
	containerDir := filepath.Join(dce.config.WorkingDirectory, containerKey)
	if err := os.MkdirAll(containerDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create container directory: %v", err)
	}

	// 准备启动脚本
	scriptPath, err := dce.prepareStartScript(containerKey, launchContext)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare start script: %v", err)
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 准备命令
	cmd := exec.CommandContext(ctx, dce.config.DefaultShell, scriptPath)
	cmd.Dir = containerDir

	// 设置环境变量
	cmd.Env = dce.prepareEnvironment(launchContext)

	// 准备输出文件
	stdoutFile, stderrFile, err := dce.prepareLogFiles(containerKey)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to prepare log files: %v", err)
	}

	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	// 设置进程组
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// 创建进程对象
	process := &DefaultContainerProcess{
		cmd:         cmd,
		containerID: containerKey,
		startTime:   time.Now(),
		logger:      dce.logger,
		stdoutFile:  stdoutFile,
		stderrFile:  stderrFile,
		ctx:         ctx,
		cancel:      cancel,
	}

	// 启动进程
	if err := cmd.Start(); err != nil {
		cancel()
		stdoutFile.Close()
		stderrFile.Close()
		return nil, fmt.Errorf("failed to start container process: %v", err)
	}

	process.pid = cmd.Process.Pid
	process.isRunning = true

	dce.logger.Info("Container process started",
		zap.String("container_id", containerKey),
		zap.Int("pid", process.pid))

	// 存储进程
	dce.mu.Lock()
	dce.processes[containerKey] = process
	dce.mu.Unlock()

	// 设置资源限制
	if dce.config.EnableCgroups {
		limits := &ResourceLimits{
			Memory:    container.Resource.Memory * 1024 * 1024, // MB to bytes
			CPUShares: int64(container.Resource.VCores * 1024),
		}
		if err := dce.SetResourceLimits(container.ID, limits); err != nil {
			dce.logger.Warn("Failed to set resource limits",
				zap.String("container_id", containerKey),
				zap.Error(err))
		}
	}

	return process, nil
}

// prepareStartScript 准备启动脚本
func (dce *DefaultContainerExecutor) prepareStartScript(containerKey string, launchContext *common.ContainerLaunchContext) (string, error) {
	scriptDir := filepath.Join(dce.config.TempDirectory, containerKey)
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return "", err
	}

	scriptPath := filepath.Join(scriptDir, "launch.sh")
	scriptFile, err := os.OpenFile(scriptPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return "", err
	}
	defer scriptFile.Close()

	// 写入启动脚本内容
	script := fmt.Sprintf(`#!/bin/bash
set -e

# 设置工作目录
cd %s

# 执行用户命令
%s
`, dce.config.WorkingDirectory, dce.buildCommand(launchContext))

	if _, err := scriptFile.WriteString(script); err != nil {
		return "", err
	}

	return scriptPath, nil
}

// buildCommand 构建执行命令
func (dce *DefaultContainerExecutor) buildCommand(launchContext *common.ContainerLaunchContext) string {
	if len(launchContext.Commands) == 0 {
		return "echo 'No commands specified'"
	}

	// 简单地连接所有命令
	return strings.Join(launchContext.Commands, " && ")
}

// prepareEnvironment 准备环境变量
func (dce *DefaultContainerExecutor) prepareEnvironment(launchContext *common.ContainerLaunchContext) []string {
	env := os.Environ()

	// 添加容器特定的环境变量
	for key, value := range launchContext.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	return env
}

// prepareLogFiles 准备日志文件
func (dce *DefaultContainerExecutor) prepareLogFiles(containerKey string) (*os.File, *os.File, error) {
	logDir := filepath.Join(dce.config.LogDirectory, containerKey)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, nil, err
	}

	stdoutFile, err := os.OpenFile(
		filepath.Join(logDir, "stdout.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		return nil, nil, err
	}

	stderrFile, err := os.OpenFile(
		filepath.Join(logDir, "stderr.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		stdoutFile.Close()
		return nil, nil, err
	}

	return stdoutFile, stderrFile, nil
}

// CleanupContainer 清理容器
func (dce *DefaultContainerExecutor) CleanupContainer(containerID common.ContainerID) error {
	containerKey := dce.getContainerKey(containerID)

	dce.mu.Lock()
	process, exists := dce.processes[containerKey]
	if exists {
		delete(dce.processes, containerKey)
	}
	dce.mu.Unlock()

	if exists {
		// 确保进程已停止
		if process.IsRunning() {
			process.Kill()
		}

		// 关闭文件
		if process.stdoutFile != nil {
			process.stdoutFile.Close()
		}
		if process.stderrFile != nil {
			process.stderrFile.Close()
		}
	}

	// 清理工作目录
	containerDir := filepath.Join(dce.config.WorkingDirectory, containerKey)
	if err := os.RemoveAll(containerDir); err != nil {
		dce.logger.Warn("Failed to remove container directory",
			zap.String("container_id", containerKey),
			zap.String("directory", containerDir),
			zap.Error(err))
	}

	// 清理临时目录
	tempDir := filepath.Join(dce.config.TempDirectory, containerKey)
	if err := os.RemoveAll(tempDir); err != nil {
		dce.logger.Warn("Failed to remove temp directory",
			zap.String("container_id", containerKey),
			zap.String("directory", tempDir),
			zap.Error(err))
	}

	dce.logger.Info("Container cleaned up",
		zap.String("container_id", containerKey))

	return nil
}

// GetContainerStatus 获取容器状态
func (dce *DefaultContainerExecutor) GetContainerStatus(containerID common.ContainerID) (*ContainerStatus, error) {
	containerKey := dce.getContainerKey(containerID)

	dce.mu.RLock()
	process, exists := dce.processes[containerKey]
	dce.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("container not found: %s", containerKey)
	}

	process.mu.RLock()
	defer process.mu.RUnlock()

	state := "UNKNOWN"
	if process.isRunning {
		state = "RUNNING"
	} else {
		if process.exitCode == 0 {
			state = "EXITED_SUCCESS"
		} else {
			state = "EXITED_FAILURE"
		}
	}

	return &ContainerStatus{
		ContainerID: containerKey,
		PID:         process.pid,
		State:       state,
		StartTime:   process.startTime,
		ExitCode:    process.exitCode,
	}, nil
}

// SetResourceLimits 设置资源限制
func (dce *DefaultContainerExecutor) SetResourceLimits(containerID common.ContainerID, limits *ResourceLimits) error {
	if !dce.config.EnableCgroups {
		return nil // 如果未启用 cgroups，则跳过
	}

	containerKey := dce.getContainerKey(containerID)

	dce.mu.RLock()
	process, exists := dce.processes[containerKey]
	dce.mu.RUnlock()

	if !exists {
		return fmt.Errorf("container not found: %s", containerKey)
	}

	// 创建 cgroup
	cgroupPath := filepath.Join(dce.config.CgroupsPath, "carrot", containerKey)
	if err := os.MkdirAll(cgroupPath, 0755); err != nil {
		return fmt.Errorf("failed to create cgroup directory: %v", err)
	}

	// 设置内存限制
	if limits.Memory > 0 {
		memoryLimitPath := filepath.Join(cgroupPath, "memory.limit_in_bytes")
		if err := dce.writeCgroupFile(memoryLimitPath, strconv.FormatInt(limits.Memory, 10)); err != nil {
			dce.logger.Warn("Failed to set memory limit", zap.Error(err))
		}
	}

	// 设置 CPU 限制
	if limits.CPUShares > 0 {
		cpuSharesPath := filepath.Join(cgroupPath, "cpu.shares")
		if err := dce.writeCgroupFile(cpuSharesPath, strconv.FormatInt(limits.CPUShares, 10)); err != nil {
			dce.logger.Warn("Failed to set CPU shares", zap.Error(err))
		}
	}

	// 将进程添加到 cgroup
	procsPath := filepath.Join(cgroupPath, "cgroup.procs")
	if err := dce.writeCgroupFile(procsPath, strconv.Itoa(process.pid)); err != nil {
		dce.logger.Warn("Failed to add process to cgroup", zap.Error(err))
	}

	dce.logger.Debug("Resource limits set",
		zap.String("container_id", containerKey),
		zap.Int64("memory", limits.Memory),
		zap.Int64("cpu_shares", limits.CPUShares))

	return nil
}

// writeCgroupFile 写入 cgroup 文件
func (dce *DefaultContainerExecutor) writeCgroupFile(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(value)
	return err
}

// getContainerKey 获取容器键
func (dce *DefaultContainerExecutor) getContainerKey(containerID common.ContainerID) string {
	return fmt.Sprintf("%d_%d_%d_%d",
		containerID.ApplicationAttemptID.ApplicationID.ClusterTimestamp,
		containerID.ApplicationAttemptID.ApplicationID.ID,
		containerID.ApplicationAttemptID.AttemptID,
		containerID.ContainerID)
}

// DefaultContainerProcess 方法实现

// GetPID 获取进程 ID
func (dcp *DefaultContainerProcess) GetPID() int {
	dcp.mu.RLock()
	defer dcp.mu.RUnlock()
	return dcp.pid
}

// IsRunning 检查进程是否在运行
func (dcp *DefaultContainerProcess) IsRunning() bool {
	dcp.mu.RLock()
	defer dcp.mu.RUnlock()
	return dcp.isRunning
}

// Kill 杀死进程
func (dcp *DefaultContainerProcess) Kill() error {
	dcp.mu.Lock()
	defer dcp.mu.Unlock()

	if !dcp.isRunning {
		return nil
	}

	dcp.logger.Info("Killing container process",
		zap.String("container_id", dcp.containerID),
		zap.Int("pid", dcp.pid))

	// 取消上下文
	dcp.cancel()

	// 杀死进程组
	if dcp.cmd.Process != nil {
		if err := syscall.Kill(-dcp.pid, syscall.SIGTERM); err != nil {
			dcp.logger.Warn("Failed to send SIGTERM", zap.Error(err))
			// 尝试强制杀死
			syscall.Kill(-dcp.pid, syscall.SIGKILL)
		}
	}

	dcp.isRunning = false
	dcp.endTime = time.Now()

	return nil
}

// Wait 等待进程完成
func (dcp *DefaultContainerProcess) Wait() error {
	if dcp.cmd == nil {
		return fmt.Errorf("no process to wait for")
	}

	err := dcp.cmd.Wait()

	dcp.mu.Lock()
	dcp.isRunning = false
	dcp.endTime = time.Now()

	if dcp.cmd.ProcessState != nil {
		dcp.exitCode = dcp.cmd.ProcessState.ExitCode()
	}
	dcp.mu.Unlock()

	// 关闭输出文件
	if dcp.stdoutFile != nil {
		dcp.stdoutFile.Close()
	}
	if dcp.stderrFile != nil {
		dcp.stderrFile.Close()
	}

	dcp.logger.Info("Container process finished",
		zap.String("container_id", dcp.containerID),
		zap.Int("exit_code", dcp.exitCode))

	return err
}

// GetExitCode 获取退出码
func (dcp *DefaultContainerProcess) GetExitCode() int {
	dcp.mu.RLock()
	defer dcp.mu.RUnlock()
	return dcp.exitCode
}

// GetLogs 获取日志
func (dcp *DefaultContainerProcess) GetLogs() (stdout, stderr string, err error) {
	// 读取标准输出日志
	if dcp.stdoutFile != nil {
		stdoutBytes, err := dcp.readLogFile(dcp.stdoutFile.Name())
		if err == nil {
			stdout = string(stdoutBytes)
		}
	}

	// 读取标准错误日志
	if dcp.stderrFile != nil {
		stderrBytes, err := dcp.readLogFile(dcp.stderrFile.Name())
		if err == nil {
			stderr = string(stderrBytes)
		}
	}

	return stdout, stderr, nil
}

// readLogFile 读取日志文件
func (dcp *DefaultContainerProcess) readLogFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 限制读取大小以防止内存过度使用
	const maxLogSize = 1024 * 1024 // 1MB

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	size := stat.Size()
	if size > maxLogSize {
		// 只读取最后的部分
		file.Seek(-maxLogSize, io.SeekEnd)
		size = maxLogSize
	}

	buffer := make([]byte, size)
	_, err = file.Read(buffer)
	return buffer, err
}

// TailLogs 实时获取日志
func (dcp *DefaultContainerProcess) TailLogs(lines int) ([]string, error) {
	if dcp.stdoutFile == nil {
		return nil, fmt.Errorf("no stdout file available")
	}

	file, err := os.Open(dcp.stdoutFile.Name())
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var result []string
	scanner := bufio.NewScanner(file)

	// 读取所有行
	var allLines []string
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	// 返回最后的 N 行
	start := len(allLines) - lines
	if start < 0 {
		start = 0
	}

	result = allLines[start:]
	return result, scanner.Err()
}
