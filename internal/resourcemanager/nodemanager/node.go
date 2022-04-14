package nodemanager

import (
	"carrot/internal/common"
	"time"
)

// Node 节点
type Node struct {
	ID                common.NodeID                `json:"id"`
	HTTPAddress       string                       `json:"http_address"`
	RackName          string                       `json:"rack_name"`
	TotalResource     common.Resource              `json:"total_resource"`
	UsedResource      common.Resource              `json:"used_resource"`
	AvailableResource common.Resource              `json:"available_resource"`
	State             string                       `json:"state"`
	LastHeartbeat     time.Time                    `json:"last_heartbeat"`
	Containers        map[string]*common.Container `json:"containers"`
}
