package recovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// StateStore 状态存储接口
type StateStore interface {
	// SaveSnapshot 保存快照
	SaveSnapshot(snapshot *ClusterSnapshot) error

	// LoadSnapshot 加载指定快照
	LoadSnapshot(id string) (*ClusterSnapshot, error)

	// LoadLatestSnapshot 加载最新快照
	LoadLatestSnapshot() (*ClusterSnapshot, error)

	// ListSnapshots 列出快照
	ListSnapshots(limit int) ([]*SnapshotMetadata, error)

	// DeleteSnapshot 删除快照
	DeleteSnapshot(id string) error

	// DeleteOldSnapshots 删除旧快照
	DeleteOldSnapshots(olderThan time.Time) error

	// Close 关闭存储
	Close() error
}

// SnapshotMetadata 快照元数据
type SnapshotMetadata struct {
	ID               string    `json:"id"`
	Timestamp        time.Time `json:"timestamp"`
	Size             int64     `json:"size"`
	ApplicationCount int       `json:"application_count"`
	NodeCount        int       `json:"node_count"`
	Version          string    `json:"version"`
}

// MemoryStateStore 内存状态存储实现
type MemoryStateStore struct {
	mu        sync.RWMutex
	snapshots map[string]*ClusterSnapshot
	metadata  map[string]*SnapshotMetadata
	logger    *zap.Logger
}

// FileStateStore 文件状态存储实现
type FileStateStore struct {
	mu        sync.RWMutex
	directory string
	logger    *zap.Logger
}

// CreateStateStore 创建状态存储
func CreateStateStore(storeType string, config map[string]interface{}) (StateStore, error) {
	switch storeType {
	case "memory":
		return NewMemoryStateStore(), nil
	case "file":
		directory, ok := config["directory"].(string)
		if !ok {
			directory = "/tmp/carrot-recovery"
		}
		return NewFileStateStore(directory)
	default:
		return nil, fmt.Errorf("unsupported state store type: %s", storeType)
	}
}

// NewMemoryStateStore 创建内存状态存储
func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{
		snapshots: make(map[string]*ClusterSnapshot),
		metadata:  make(map[string]*SnapshotMetadata),
		logger:    zap.NewNop(), // 使用空logger，实际使用时可以注入
	}
}

// SaveSnapshot 保存快照到内存
func (ms *MemoryStateStore) SaveSnapshot(snapshot *ClusterSnapshot) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	id := generateSnapshotID(snapshot.Timestamp)

	// 深拷贝快照以避免并发修改
	snapshotCopy := *snapshot
	ms.snapshots[id] = &snapshotCopy

	// 创建元数据
	metadata := &SnapshotMetadata{
		ID:               id,
		Timestamp:        snapshot.Timestamp,
		ApplicationCount: len(snapshot.Applications),
		NodeCount:        len(snapshot.Nodes),
		Version:          snapshot.Version,
	}

	// 计算大小（近似）
	if data, err := json.Marshal(snapshot); err == nil {
		metadata.Size = int64(len(data))
	}

	ms.metadata[id] = metadata

	ms.logger.Debug("Snapshot saved to memory",
		zap.String("id", id),
		zap.Time("timestamp", snapshot.Timestamp))

	return nil
}

// LoadSnapshot 从内存加载快照
func (ms *MemoryStateStore) LoadSnapshot(id string) (*ClusterSnapshot, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	snapshot, exists := ms.snapshots[id]
	if !exists {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}

	// 返回副本以避免外部修改
	snapshotCopy := *snapshot
	return &snapshotCopy, nil
}

// LoadLatestSnapshot 从内存加载最新快照
func (ms *MemoryStateStore) LoadLatestSnapshot() (*ClusterSnapshot, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var latestID string
	var latestTime time.Time

	for id, metadata := range ms.metadata {
		if metadata.Timestamp.After(latestTime) {
			latestTime = metadata.Timestamp
			latestID = id
		}
	}

	if latestID == "" {
		return nil, nil // 没有快照
	}

	return ms.LoadSnapshot(latestID)
}

// ListSnapshots 列出内存中的快照
func (ms *MemoryStateStore) ListSnapshots(limit int) ([]*SnapshotMetadata, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var metadataList []*SnapshotMetadata
	for _, metadata := range ms.metadata {
		metadataList = append(metadataList, metadata)
	}

	// 按时间戳降序排序
	sort.Slice(metadataList, func(i, j int) bool {
		return metadataList[i].Timestamp.After(metadataList[j].Timestamp)
	})

	// 应用限制
	if limit > 0 && len(metadataList) > limit {
		metadataList = metadataList[:limit]
	}

	return metadataList, nil
}

// DeleteSnapshot 从内存删除快照
func (ms *MemoryStateStore) DeleteSnapshot(id string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if _, exists := ms.snapshots[id]; !exists {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	delete(ms.snapshots, id)
	delete(ms.metadata, id)

	ms.logger.Debug("Snapshot deleted from memory", zap.String("id", id))
	return nil
}

// DeleteOldSnapshots 从内存删除旧快照
func (ms *MemoryStateStore) DeleteOldSnapshots(olderThan time.Time) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var deletedCount int
	for id, metadata := range ms.metadata {
		if metadata.Timestamp.Before(olderThan) {
			delete(ms.snapshots, id)
			delete(ms.metadata, id)
			deletedCount++
		}
	}

	ms.logger.Debug("Old snapshots deleted from memory",
		zap.Int("deleted_count", deletedCount),
		zap.Time("older_than", olderThan))

	return nil
}

// Close 关闭内存状态存储
func (ms *MemoryStateStore) Close() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.snapshots = make(map[string]*ClusterSnapshot)
	ms.metadata = make(map[string]*SnapshotMetadata)

	return nil
}

// NewFileStateStore 创建文件状态存储
func NewFileStateStore(directory string) (*FileStateStore, error) {
	// 确保目录存在
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", directory, err)
	}

	return &FileStateStore{
		directory: directory,
		logger:    zap.NewNop(), // 使用空logger，实际使用时可以注入
	}, nil
}

// SaveSnapshot 保存快照到文件
func (fs *FileStateStore) SaveSnapshot(snapshot *ClusterSnapshot) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	id := generateSnapshotID(snapshot.Timestamp)
	filename := filepath.Join(fs.directory, fmt.Sprintf("%s.json", id))

	// 序列化快照
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}

	fs.logger.Debug("Snapshot saved to file",
		zap.String("id", id),
		zap.String("filename", filename),
		zap.Time("timestamp", snapshot.Timestamp))

	return nil
}

// LoadSnapshot 从文件加载快照
func (fs *FileStateStore) LoadSnapshot(id string) (*ClusterSnapshot, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	filename := filepath.Join(fs.directory, fmt.Sprintf("%s.json", id))

	// 检查文件是否存在
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}

	// 读取文件
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	// 反序列化快照
	var snapshot ClusterSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// LoadLatestSnapshot 从文件加载最新快照
func (fs *FileStateStore) LoadLatestSnapshot() (*ClusterSnapshot, error) {
	metadataList, err := fs.ListSnapshots(1)
	if err != nil {
		return nil, err
	}

	if len(metadataList) == 0 {
		return nil, nil // 没有快照
	}

	return fs.LoadSnapshot(metadataList[0].ID)
}

// ListSnapshots 列出文件中的快照
func (fs *FileStateStore) ListSnapshots(limit int) ([]*SnapshotMetadata, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var metadataList []*SnapshotMetadata
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			id := strings.TrimSuffix(file.Name(), ".json")

			// 解析时间戳
			timestamp, err := parseSnapshotID(id)
			if err != nil {
				fs.logger.Warn("Failed to parse snapshot ID",
					zap.String("id", id), zap.Error(err))
				continue
			}

			metadata := &SnapshotMetadata{
				ID:        id,
				Timestamp: timestamp,
			}

			// 获取文件信息以获取大小
			if fileInfo, err := file.Info(); err == nil {
				metadata.Size = fileInfo.Size()
			}

			// 尝试读取快照以获取更多元数据
			if snapshot, err := fs.LoadSnapshot(id); err == nil {
				metadata.ApplicationCount = len(snapshot.Applications)
				metadata.NodeCount = len(snapshot.Nodes)
				metadata.Version = snapshot.Version
			}

			metadataList = append(metadataList, metadata)
		}
	}

	// 按时间戳降序排序
	sort.Slice(metadataList, func(i, j int) bool {
		return metadataList[i].Timestamp.After(metadataList[j].Timestamp)
	})

	// 应用限制
	if limit > 0 && len(metadataList) > limit {
		metadataList = metadataList[:limit]
	}

	return metadataList, nil
}

// DeleteSnapshot 删除文件快照
func (fs *FileStateStore) DeleteSnapshot(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	filename := filepath.Join(fs.directory, fmt.Sprintf("%s.json", id))

	// 检查文件是否存在
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	// 删除文件
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("failed to delete snapshot file: %w", err)
	}

	fs.logger.Debug("Snapshot deleted from file",
		zap.String("id", id),
		zap.String("filename", filename))

	return nil
}

// DeleteOldSnapshots 删除文件中的旧快照
func (fs *FileStateStore) DeleteOldSnapshots(olderThan time.Time) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	files, err := os.ReadDir(fs.directory)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var deletedCount int
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			id := strings.TrimSuffix(file.Name(), ".json")

			// 解析时间戳
			timestamp, err := parseSnapshotID(id)
			if err != nil {
				continue
			}

			if timestamp.Before(olderThan) {
				filename := filepath.Join(fs.directory, file.Name())
				if err := os.Remove(filename); err == nil {
					deletedCount++
				}
			}
		}
	}

	fs.logger.Debug("Old snapshots deleted from file",
		zap.Int("deleted_count", deletedCount),
		zap.Time("older_than", olderThan))

	return nil
}

// Close 关闭文件状态存储
func (fs *FileStateStore) Close() error {
	// 文件存储不需要特殊的清理操作
	return nil
}

// generateSnapshotID 生成快照ID
func generateSnapshotID(timestamp time.Time) string {
	return fmt.Sprintf("snapshot_%d", timestamp.UnixNano())
}

// parseSnapshotID 解析快照ID
func parseSnapshotID(id string) (time.Time, error) {
	if !strings.HasPrefix(id, "snapshot_") {
		return time.Time{}, fmt.Errorf("invalid snapshot ID format: %s", id)
	}

	timestampStr := strings.TrimPrefix(id, "snapshot_")
	var nanos int64
	if _, err := fmt.Sscanf(timestampStr, "%d", &nanos); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	return time.Unix(0, nanos), nil
}
