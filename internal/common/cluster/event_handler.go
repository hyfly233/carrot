package cluster

import (
	"sync"

	"carrot/internal/common"
	"go.uber.org/zap"
)

// DefaultEventHandler 默认事件处理器实现
type DefaultEventHandler struct {
	config     common.ClusterConfig
	logger     *zap.Logger
	processors map[common.ClusterEventType][]func(common.ClusterEvent) error
	mutex      sync.RWMutex
}

// NewDefaultEventHandler 创建默认事件处理器
func NewDefaultEventHandler(config common.ClusterConfig, logger *zap.Logger) (*DefaultEventHandler, error) {
	return &DefaultEventHandler{
		config:     config,
		logger:     logger.With(zap.String("component", "event_handler")),
		processors: make(map[common.ClusterEventType][]func(common.ClusterEvent) error),
	}, nil
}

// HandleEvent 处理事件
func (eh *DefaultEventHandler) HandleEvent(event common.ClusterEvent) error {
	eh.mutex.RLock()
	processors, exists := eh.processors[event.Type]
	eh.mutex.RUnlock()

	if !exists {
		eh.logger.Debug("No processors for event type",
			zap.String("type", string(event.Type)))
		return nil
	}

	eh.logger.Debug("Processing event",
		zap.String("type", string(event.Type)),
		zap.String("source", event.Source.String()))

	// 执行所有注册的处理器
	for _, processor := range processors {
		if err := processor(event); err != nil {
			eh.logger.Error("Event processor failed",
				zap.String("type", string(event.Type)),
				zap.Error(err))
		}
	}

	return nil
}

// RegisterEventProcessor 注册事件处理器
func (eh *DefaultEventHandler) RegisterEventProcessor(eventType common.ClusterEventType,
	processor func(common.ClusterEvent) error) {

	eh.mutex.Lock()
	defer eh.mutex.Unlock()

	if _, exists := eh.processors[eventType]; !exists {
		eh.processors[eventType] = make([]func(common.ClusterEvent) error, 0)
	}

	eh.processors[eventType] = append(eh.processors[eventType], processor)

	eh.logger.Info("Event processor registered",
		zap.String("type", string(eventType)))
}
