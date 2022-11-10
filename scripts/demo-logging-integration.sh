#!/bin/bash

# 测试 Kafka 和 Doris 日志集成的演示脚本
echo "=== Carrot YARN 日志系统 Kafka/Doris 集成演示 ==="

# 创建启用 Kafka 和 Doris 的测试配置
echo "创建测试配置文件..."

# 创建启用 Kafka 的 ResourceManager 配置
cat > configs/resourcemanager-kafka.yaml << 'EOF'
cluster:
  name: "carrot-cluster"
  id: "rm-1"
  discovery_method: "static"

resourcemanager:
  port: 8088
  address: "0.0.0.0"
  heartbeat_interval: 3s
  node_expiry_timeout: 600s
  application_max_attempts: 2

scheduler:
  type: "fifo"
  max_apps: 10000

log:
  level: "info"
  development: false
  file_output:
    enabled: true
    directory: "logs/resourcemanager"
    max_file_size: "100MB"
    max_backups: 168
    max_age: 7
    compress: true
    hourly_rotation: true
    daily_compression: true
  kafka_output:
    enabled: true
    brokers: ["localhost:9092"]
    topic: "carrot-rm-logs"
    batch_size: 10
    timeout: "5s"
    retries: 3
  doris_output:
    enabled: true
    stream_load_url: "http://localhost:8030/api/carrot_logs/rm_logs/_stream_load"
    database: "carrot_logs"
    table: "rm_logs"
    username: "root"
    password: ""
    batch_size: 20
    flush_interval: "10s"
  console_output:
    enabled: true
    colorized: false
EOF

echo "✅ 创建了启用 Kafka/Doris 的测试配置"

# 显示配置文件内容
echo ""
echo "=== 测试配置示例 ==="
echo "Kafka 输出配置："
echo "  - enabled: true"
echo "  - brokers: [localhost:9092]"
echo "  - topic: carrot-rm-logs"
echo "  - batch_size: 10"
echo ""
echo "Doris 输出配置："
echo "  - enabled: true"
echo "  - stream_load_url: http://localhost:8030/api/carrot_logs/rm_logs/_stream_load"
echo "  - database: carrot_logs"
echo "  - table: rm_logs"
echo "  - batch_size: 20"

# 模拟启动（不真正启动，因为 Kafka/Doris 可能不存在）
echo ""
echo "=== 模拟启动测试 ==="
echo "注意：此演示展示配置，实际 Kafka/Doris 服务需要单独部署"

echo ""
echo "编译项目..."
go build -o bin/resourcemanager ./cmd/resourcemanager/

if [ $? -ne 0 ]; then
    echo "❌ 编译失败"
    exit 1
fi

echo "✅ 编译成功"

echo ""
echo "=== 测试日志组件加载 ==="
echo "启动 ResourceManager 测试配置加载..."

# 短暂启动检查配置加载
./bin/resourcemanager -config=configs/resourcemanager-kafka.yaml &
PID=$!

sleep 3
kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true

echo ""
echo "检查日志文件..."
if [ -f "logs/resourcemanager/carrot-$(date +%Y-%m-%d-%H).log" ]; then
    echo "✅ 日志文件创建成功"
    echo "最新日志条目样本："
    tail -2 "logs/resourcemanager/carrot-$(date +%Y-%m-%d-%H).log" | jq .
else
    echo "❌ 日志文件未创建"
fi

echo ""
echo "=== 集成部署指南 ==="
echo ""
echo "1. 部署 Kafka:"
echo "   # 启动 Zookeeper"
echo "   zookeeper-server-start.sh config/zookeeper.properties"
echo ""
echo "   # 启动 Kafka"
echo "   kafka-server-start.sh config/server.properties"
echo ""
echo "   # 创建日志主题"
echo "   kafka-topics.sh --create --topic carrot-rm-logs --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1"
echo "   kafka-topics.sh --create --topic carrot-nm-logs --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1"
echo "   kafka-topics.sh --create --topic carrot-am-logs --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1"
echo ""
echo "2. 部署 Doris:"
echo "   # 启动 FE (Frontend)"
echo "   /opt/doris/fe/bin/start_fe.sh --daemon"
echo ""
echo "   # 启动 BE (Backend)"
echo "   /opt/doris/be/bin/start_be.sh --daemon"
echo ""
echo "   # 创建数据库和表"
echo "   mysql -h127.0.0.1 -P9030 -uroot -p"
echo ""
echo "3. 创建 Doris 表结构:"
echo "   CREATE DATABASE IF NOT EXISTS carrot_logs;"
echo "   USE carrot_logs;"
echo ""
echo "   CREATE TABLE IF NOT EXISTS rm_logs ("
echo "       timestamp DATETIME,"
echo "       level VARCHAR(10),"
echo "       component VARCHAR(50),"
echo "       message TEXT,"
echo "       fields JSON"
echo "   ) ENGINE=OLAP"
echo "   DUPLICATE KEY(timestamp, level, component)"
echo "   PARTITION BY RANGE(timestamp) ()"
echo "   DISTRIBUTED BY HASH(timestamp) BUCKETS 8;"
echo ""
echo "4. 启用日志集成:"
echo "   # 修改配置文件启用 Kafka 和 Doris"
echo "   vim configs/resourcemanager.yaml"
echo "   # 设置 kafka_output.enabled: true"
echo "   # 设置 doris_output.enabled: true"
echo ""
echo "5. 监控日志流:"
echo "   # 监控 Kafka 消息"
echo "   kafka-console-consumer.sh --topic carrot-rm-logs --bootstrap-server localhost:9092"
echo ""
echo "   # 查询 Doris 日志"
echo "   SELECT * FROM carrot_logs.rm_logs ORDER BY timestamp DESC LIMIT 10;"

echo ""
echo "=== 性能配置建议 ==="
echo ""
echo "生产环境优化："
echo "  • 文件日志："
echo "    - directory: /var/log/carrot"
echo "    - max_file_size: 500MB"
echo "    - max_backups: 336  # 14天 * 24小时"
echo "    - max_age: 14"
echo ""
echo "  • Kafka 配置："
echo "    - batch_size: 1000"
echo "    - timeout: 30s"
echo "    - retries: 5"
echo ""
echo "  • Doris 配置："
echo "    - batch_size: 5000"
echo "    - flush_interval: 60s"

echo ""
echo "=== 故障排除指南 ==="
echo ""
echo "常见问题:"
echo "1. Kafka 连接失败："
echo "   - 检查 Kafka 服务是否启动"
echo "   - 验证 brokers 地址配置"
echo "   - 确认防火墙设置"
echo ""
echo "2. Doris 写入失败："
echo "   - 检查 Stream Load URL"
echo "   - 验证用户名密码"
echo "   - 确认表结构匹配"
echo ""
echo "3. 日志文件轮转问题："
echo "   - 检查磁盘空间"
echo "   - 验证目录权限"
echo "   - 监控文件大小"

echo ""
echo "=== 演示完成 ==="
echo "查看详细文档：docs/日志系统集成指南.md"

# 清理测试配置
rm -f configs/resourcemanager-kafka.yaml
