package common

import (
	"errors"
	"fmt"
)

// 定义常见错误类型
var (
	ErrInvalidParameter     = errors.New("invalid parameter")
	ErrResourceNotFound     = errors.New("resource not found")
	ErrResourceUnavailable  = errors.New("resource unavailable")
	ErrInvalidState         = errors.New("invalid state")
	ErrOperationTimeout     = errors.New("operation timeout")
	ErrPermissionDenied     = errors.New("permission denied")
	ErrServiceUnavailable   = errors.New("service unavailable")
	ErrInvalidConfiguration = errors.New("invalid configuration")
)

// CarrotError 自定义错误类型
type CarrotError struct {
	Type    string `json:"type"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
	Cause   error  `json:"-"`
}

func (e *CarrotError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Type, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *CarrotError) Unwrap() error {
	return e.Cause
}

// NewCarrotError 创建新的Carrot错误
func NewCarrotError(errorType string, code int, message string, details string) *CarrotError {
	return &CarrotError{
		Type:    errorType,
		Code:    code,
		Message: message,
		Details: details,
	}
}

// ValidationError 验证错误
type ValidationError struct {
	Field   string      `json:"field"`
	Message string      `json:"message"`
	Value   interface{} `json:"value,omitempty"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field '%s': %s", e.Field, e.Message)
}

// NewValidationError 创建验证错误
func NewValidationError(field, message string, value interface{}) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	}
}

// ValidateResource 验证资源配置
func ValidateResource(resource Resource) error {
	if resource.Memory <= 0 {
		return NewValidationError("memory", "must be greater than 0", resource.Memory)
	}
	if resource.VCores <= 0 {
		return NewValidationError("vcores", "must be greater than 0", resource.VCores)
	}
	if resource.Memory > 1024*1024 { // 1TB
		return NewValidationError("memory", "exceeds maximum limit (1TB)", resource.Memory)
	}
	if resource.VCores > 1000 {
		return NewValidationError("vcores", "exceeds maximum limit (1000)", resource.VCores)
	}
	return nil
}

// ValidateApplicationSubmissionContext 验证应用程序提交上下文
func ValidateApplicationSubmissionContext(ctx ApplicationSubmissionContext) error {
	if ctx.ApplicationName == "" {
		return NewValidationError("application_name", "cannot be empty", ctx.ApplicationName)
	}
	if ctx.Queue == "" {
		return NewValidationError("queue", "cannot be empty", ctx.Queue)
	}
	if err := ValidateResource(ctx.Resource); err != nil {
		return fmt.Errorf("invalid resource: %w", err)
	}
	if ctx.MaxAppAttempts <= 0 || ctx.MaxAppAttempts > 10 {
		return NewValidationError("max_app_attempts", "must be between 1 and 10", ctx.MaxAppAttempts)
	}
	if len(ctx.AMContainerSpec.Commands) == 0 {
		return NewValidationError("am_container_spec.commands", "cannot be empty", ctx.AMContainerSpec.Commands)
	}
	return nil
}

// ValidateNodeID 验证节点ID
func ValidateNodeID(nodeID NodeID) error {
	if nodeID.Host == "" {
		return NewValidationError("host", "cannot be empty", nodeID.Host)
	}
	if nodeID.Port <= 0 || nodeID.Port > 65535 {
		return NewValidationError("port", "must be between 1 and 65535", nodeID.Port)
	}
	return nil
}

// ValidateContainerLaunchContext 验证容器启动上下文
func ValidateContainerLaunchContext(ctx ContainerLaunchContext) error {
	if len(ctx.Commands) == 0 {
		return NewValidationError("commands", "cannot be empty", ctx.Commands)
	}
	for i, cmd := range ctx.Commands {
		if cmd == "" {
			return NewValidationError(fmt.Sprintf("commands[%d]", i), "command cannot be empty", cmd)
		}
	}
	return nil
}
