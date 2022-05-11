package scheduler

import (
	"carrot/internal/common"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateScheduler_FIFO(t *testing.T) {
	config := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "fifo",
		},
	}

	scheduler, err := CreateScheduler(config)

	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}

func TestCreateScheduler_Capacity(t *testing.T) {
	config := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "capacity",
		},
	}

	scheduler, err := CreateScheduler(config)

	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}

func TestCreateScheduler_Fair(t *testing.T) {
	config := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "fair",
		},
	}

	scheduler, err := CreateScheduler(config)

	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}

func TestCreateScheduler_InvalidType(t *testing.T) {
	config := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "invalid",
		},
	}

	scheduler, err := CreateScheduler(config)

	assert.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "unsupported scheduler type")
}

func TestCreateScheduler_EmptyType(t *testing.T) {
	config := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "",
		},
	}

	scheduler, err := CreateScheduler(config)

	// 应该回退到默认的FIFO调度器
	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}

func TestCreateScheduler_NilConfig(t *testing.T) {
	scheduler, err := CreateScheduler(nil)

	// 应该回退到默认的FIFO调度器
	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}

func TestCreateScheduler_CaseInsensitive(t *testing.T) {
	testCases := []struct {
		name          string
		schedulerType string
		expectError   bool
	}{
		{"FIFO uppercase", "FIFO", false},
		{"Capacity mixed case", "Capacity", false},
		{"Fair mixed case", "Fair", false},
		{"fifo lowercase", "fifo", false},
		{"capacity lowercase", "capacity", false},
		{"fair lowercase", "fair", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &common.Config{
				Scheduler: common.SchedulerConfig{
					Type: tc.schedulerType,
				},
			}

			scheduler, err := CreateScheduler(config)

			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, scheduler)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, scheduler)
			}
		})
	}
}

func TestGetSupportedSchedulerTypes(t *testing.T) {
	supportedTypes := []string{"fifo", "capacity", "fair"}

	for _, schedulerType := range supportedTypes {
		config := &common.Config{
			Scheduler: common.SchedulerConfig{
				Type: schedulerType,
			},
		}

		scheduler, err := CreateScheduler(config)

		assert.NoError(t, err, "Scheduler type %s should be supported", schedulerType)
		assert.NotNil(t, scheduler, "Scheduler should not be nil for type %s", schedulerType)
	}
}

func TestCreateScheduler_DefaultFallback(t *testing.T) {
	testCases := []struct {
		name   string
		config *common.Config
	}{
		{
			name:   "Nil config",
			config: nil,
		},
		{
			name: "Empty scheduler type",
			config: &common.Config{
				Scheduler: common.SchedulerConfig{
					Type: "",
				},
			},
		},
		{
			name: "Whitespace scheduler type",
			config: &common.Config{
				Scheduler: common.SchedulerConfig{
					Type: "   ",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheduler, err := CreateScheduler(tc.config)

			require.NoError(t, err)
			assert.NotNil(t, scheduler)
			// 应该创建默认的FIFO调度器
		})
	}
}
