package common

// Resource 表示资源配置
type Resource struct {
	Memory int64 `json:"memory"` // MB
	VCores int32 `json:"vcores"` // 虚拟核心数
}
