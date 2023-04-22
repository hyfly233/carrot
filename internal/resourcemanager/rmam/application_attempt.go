package rmam

import (
	"carrot/internal/common"
	"time"
)

// ApplicationAttempt 应用程序尝试
type ApplicationAttempt struct {
	ID          common.ApplicationAttemptID `json:"id"`
	State       string                      `json:"state"`
	StartTime   time.Time                   `json:"start_time"`
	FinishTime  time.Time                   `json:"finish_time,omitempty"`
	AMContainer *common.Container           `json:"am_container,omitempty"`
	TrackingURL string                      `json:"tracking_url"`
}
