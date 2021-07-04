package common

// Resource 表示资源配置
type Resource struct {
	Memory int64 `json:"memory"` // MB
	VCores int32 `json:"vcores"` // 虚拟核心数
}

// NodeID 节点标识
type NodeID struct {
	Host string `json:"host"`
	Port int32  `json:"port"`
}
