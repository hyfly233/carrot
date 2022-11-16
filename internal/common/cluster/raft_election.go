package cluster

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"carrot/internal/common"
	"go.uber.org/zap"
)

// RaftElection Raft算法选举实现
type RaftElection struct {
	config         common.ClusterConfig
	localNode      *common.ClusterNode
	clusterManager *ClusterManager
	logger         *zap.Logger

	// Raft状态
	state         ElectionState
	currentTerm   int64
	votedFor      *common.NodeID
	lastHeartbeat time.Time

	// 选举相关
	votes          map[string]bool
	leaderID       *common.NodeID
	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	// 回调函数
	onLeaderChange []func(old, new *common.NodeID)

	// 同步
	mutex    sync.RWMutex
	stopChan chan struct{}
}

// ElectionState 选举状态
type ElectionState int

const (
	StateFollower ElectionState = iota
	StateCandidate
	StateLeader
)

// NewRaftElection 创建Raft选举实例
func NewRaftElection(config common.ClusterConfig, localNode *common.ClusterNode,
	clusterManager *ClusterManager, logger *zap.Logger) (*RaftElection, error) {

	return &RaftElection{
		config:         config,
		localNode:      localNode,
		clusterManager: clusterManager,
		logger:         logger.With(zap.String("component", "raft_election")),
		state:          StateFollower,
		votes:          make(map[string]bool),
		onLeaderChange: make([]func(old, new *common.NodeID), 0),
		stopChan:       make(chan struct{}),
	}, nil
}

// StartElection 启动选举
func (re *RaftElection) StartElection() error {
	re.mutex.Lock()
	defer re.mutex.Unlock()

	re.logger.Info("Starting election", zap.Int64("term", re.currentTerm+1))

	// 增加任期
	re.currentTerm++

	// 转换为候选者状态
	re.state = StateCandidate
	re.votedFor = &re.localNode.ID
	re.votes = make(map[string]bool)
	re.votes[re.localNode.ID.String()] = true // 给自己投票

	// 重置选举定时器
	re.resetElectionTimer()

	// 向其他节点请求投票
	go re.requestVotes()

	return nil
}

// Stop 停止选举
func (re *RaftElection) Stop() error {
	re.logger.Info("Stopping election")
	close(re.stopChan)

	if re.electionTimer != nil {
		re.electionTimer.Stop()
	}
	if re.heartbeatTimer != nil {
		re.heartbeatTimer.Stop()
	}

	return nil
}

// StepDown 主动放弃领导权
func (re *RaftElection) StepDown() error {
	re.mutex.Lock()
	defer re.mutex.Unlock()

	if re.state != StateLeader {
		return fmt.Errorf("not a leader")
	}

	re.logger.Info("Stepping down as leader")

	oldLeader := re.leaderID

	// 转换为跟随者
	re.state = StateFollower
	re.leaderID = nil

	if re.heartbeatTimer != nil {
		re.heartbeatTimer.Stop()
	}

	// 重置选举定时器
	re.resetElectionTimer()

	// 通知回调
	re.notifyLeaderChange(oldLeader, nil)

	return nil
}

// IsLeader 检查是否为领导者
func (re *RaftElection) IsLeader() bool {
	re.mutex.RLock()
	defer re.mutex.RUnlock()
	return re.state == StateLeader
}

// GetLeader 获取当前领导者
func (re *RaftElection) GetLeader() (*common.NodeID, error) {
	re.mutex.RLock()
	defer re.mutex.RUnlock()

	if re.leaderID == nil {
		return nil, fmt.Errorf("no leader elected")
	}

	return re.leaderID, nil
}

// OnLeaderChange 注册领导者变更回调
func (re *RaftElection) OnLeaderChange(callback func(old, new *common.NodeID)) {
	re.onLeaderChange = append(re.onLeaderChange, callback)
}

// HandleVoteRequest 处理投票请求
func (re *RaftElection) HandleVoteRequest(term int64, candidateID common.NodeID) bool {
	re.mutex.Lock()
	defer re.mutex.Unlock()

	// 如果请求的任期小于当前任期，拒绝投票
	if term < re.currentTerm {
		return false
	}

	// 如果请求的任期大于当前任期，更新任期并转换为跟随者
	if term > re.currentTerm {
		re.currentTerm = term
		re.votedFor = nil
		re.state = StateFollower
	}

	// 如果还没有投票或者已经投给了该候选者，投票
	if re.votedFor == nil || re.votedFor.String() == candidateID.String() {
		re.votedFor = &candidateID
		re.lastHeartbeat = time.Now()

		re.logger.Info("Voted for candidate",
			zap.String("candidate", candidateID.String()),
			zap.Int64("term", term))

		return true
	}

	return false
}

// HandleHeartbeat 处理心跳
func (re *RaftElection) HandleHeartbeat(term int64, leaderID common.NodeID) {
	re.mutex.Lock()
	defer re.mutex.Unlock()

	// 如果心跳的任期大于等于当前任期，接受领导者
	if term >= re.currentTerm {
		re.currentTerm = term
		re.state = StateFollower

		oldLeader := re.leaderID
		re.leaderID = &leaderID
		re.lastHeartbeat = time.Now()

		// 重置选举定时器
		re.resetElectionTimer()

		// 如果领导者发生变化，通知回调
		if oldLeader == nil || oldLeader.String() != leaderID.String() {
			re.notifyLeaderChange(oldLeader, &leaderID)
		}
	}
}

// 私有方法

func (re *RaftElection) requestVotes() {
	nodes := re.clusterManager.GetNodes()

	for nodeID, node := range nodes {
		if nodeID == re.localNode.ID.String() {
			continue // 跳过自己
		}

		go re.sendVoteRequest(node)
	}

	// 等待投票结果
	re.checkElectionResult()
}

func (re *RaftElection) sendVoteRequest(node *common.ClusterNode) {
	// 这里应该发送实际的网络请求
	// 为了简化，我们假设其他节点会调用 HandleVoteRequest

	// 模拟网络延迟和投票响应
	time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)

	// 模拟投票结果 (在实际实现中，这应该通过网络通信实现)
	if re.shouldVoteFor(node) {
		re.mutex.Lock()
		re.votes[node.ID.String()] = true
		re.mutex.Unlock()
	}
}

func (re *RaftElection) shouldVoteFor(node *common.ClusterNode) bool {
	// 简化的投票逻辑：如果节点是活跃的，就投票
	return node.State == common.NodeStateActive
}

func (re *RaftElection) checkElectionResult() {
	time.Sleep(re.config.ElectionTimeout / 2) // 等待一半的选举超时时间

	re.mutex.Lock()
	defer re.mutex.Unlock()

	if re.state != StateCandidate {
		return // 已经不是候选者了
	}

	nodes := re.clusterManager.GetNodes()
	majorityNeeded := len(nodes)/2 + 1
	votesReceived := len(re.votes)

	re.logger.Debug("Election result",
		zap.Int("votes_received", votesReceived),
		zap.Int("majority_needed", majorityNeeded))

	if votesReceived >= majorityNeeded {
		// 赢得选举，成为领导者
		re.becomeLeader()
	} else {
		// 没有赢得选举，回到跟随者状态
		re.state = StateFollower
		re.resetElectionTimer()
	}
}

func (re *RaftElection) becomeLeader() {
	re.logger.Info("Became leader", zap.Int64("term", re.currentTerm))

	oldLeader := re.leaderID
	re.state = StateLeader
	re.leaderID = &re.localNode.ID

	// 停止选举定时器
	if re.electionTimer != nil {
		re.electionTimer.Stop()
	}

	// 开始发送心跳
	re.startHeartbeat()

	// 通知回调
	re.notifyLeaderChange(oldLeader, &re.localNode.ID)

	// 发布领导者选举事件
	re.clusterManager.publishEvent(common.ClusterEvent{
		Type:      common.ClusterEventLeaderElected,
		Source:    re.localNode.ID,
		Timestamp: time.Now(),
		Severity:  common.EventSeverityInfo,
	})
}

func (re *RaftElection) startHeartbeat() {
	re.heartbeatTimer = time.NewTimer(re.config.HeartbeatInterval)

	go func() {
		for {
			select {
			case <-re.heartbeatTimer.C:
				if re.state == StateLeader {
					re.sendHeartbeats()
					re.heartbeatTimer.Reset(re.config.HeartbeatInterval)
				}
			case <-re.stopChan:
				return
			}
		}
	}()
}

func (re *RaftElection) sendHeartbeats() {
	nodes := re.clusterManager.GetNodes()

	for nodeID, node := range nodes {
		if nodeID == re.localNode.ID.String() {
			continue // 跳过自己
		}

		go re.sendHeartbeat(node)
	}
}

func (re *RaftElection) sendHeartbeat(node *common.ClusterNode) {
	// 这里应该发送实际的心跳网络请求
	// 为了简化，我们假设心跳总是成功的

	re.logger.Debug("Sent heartbeat", zap.String("node", node.ID.String()))
}

func (re *RaftElection) resetElectionTimer() {
	timeout := re.config.ElectionTimeout +
		time.Duration(rand.Intn(int(re.config.ElectionTimeout/time.Millisecond)))*time.Millisecond

	if re.electionTimer != nil {
		re.electionTimer.Stop()
	}

	re.electionTimer = time.NewTimer(timeout)

	go func() {
		select {
		case <-re.electionTimer.C:
			if re.state == StateFollower {
				// 选举超时，开始新的选举
				re.StartElection()
			}
		case <-re.stopChan:
			return
		}
	}()
}

func (re *RaftElection) notifyLeaderChange(old, new *common.NodeID) {
	for _, callback := range re.onLeaderChange {
		go callback(old, new)
	}
}
