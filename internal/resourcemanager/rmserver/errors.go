package rmserver

import "errors"

var (
	// ErrServerNotFound 服务器未找到错误
	ErrServerNotFound = errors.New("rmserver not found")

	// ErrServerAlreadyRunning 服务器已在运行错误
	ErrServerAlreadyRunning = errors.New("rmserver already running")

	// ErrServerNotRunning 服务器未运行错误
	ErrServerNotRunning = errors.New("rmserver not running")

	// ErrInvalidPort 无效端口错误
	ErrInvalidPort = errors.New("invalid port")

	// ErrNotImplemented 功能未实现错误
	ErrNotImplemented = errors.New("not implemented")

	// ErrInvalidServerType 无效服务器类型错误
	ErrInvalidServerType = errors.New("invalid rmserver type")
)
