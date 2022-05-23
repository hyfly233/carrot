package main

import (
	"carrot/internal/common"
	"carrot/internal/resourcemanager"
	"fmt"
)

func main() {
	// 测试不同调度器类型的创建

	// 测试FIFO调度器
	fmt.Println("=== 测试FIFO调度器 ===")
	fifoConfig := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "fifo",
		},
	}
	fifoRM := resourcemanager.NewResourceManager(fifoConfig)
	fmt.Printf("FIFO ResourceManager 创建成功: %v\n", fifoRM != nil)

	// 测试Capacity调度器
	fmt.Println("\n=== 测试Capacity调度器 ===")
	capacityConfig := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "capacity",
			CapacityScheduler: &common.CapacitySchedulerConfig{
				Queues: map[string]*common.CapacityQueueConfig{
					"root": {
						Name:        "root",
						Type:        "parent",
						Capacity:    100.0,
						MaxCapacity: 100.0,
						Children: map[string]*common.CapacityQueueConfig{
							"default": {
								Name:        "default",
								Type:        "leaf",
								Capacity:    50.0,
								MaxCapacity: 80.0,
							},
							"production": {
								Name:        "production",
								Type:        "leaf",
								Capacity:    50.0,
								MaxCapacity: 90.0,
							},
						},
					},
				},
			},
		},
	}
	capacityRM := resourcemanager.NewResourceManager(capacityConfig)
	fmt.Printf("Capacity ResourceManager 创建成功: %v\n", capacityRM != nil)

	// 测试Fair调度器
	fmt.Println("\n=== 测试Fair调度器 ===")
	fairConfig := &common.Config{
		Scheduler: common.SchedulerConfig{
			Type: "fair",
			FairScheduler: &common.FairSchedulerConfig{
				Queues: map[string]*common.FairQueueConfig{
					"root": {
						Name:   "root",
						Type:   "parent",
						Weight: 1.0,
						Children: map[string]*common.FairQueueConfig{
							"default": {
								Name:   "default",
								Type:   "leaf",
								Weight: 1.0,
							},
							"production": {
								Name:   "production",
								Type:   "leaf",
								Weight: 2.0,
							},
						},
					},
				},
			},
		},
	}
	fairRM := resourcemanager.NewResourceManager(fairConfig)
	fmt.Printf("Fair ResourceManager 创建成功: %v\n", fairRM != nil)

	// 测试默认配置
	fmt.Println("\n=== 测试默认配置 ===")
	defaultRM := resourcemanager.NewResourceManager(nil)
	fmt.Printf("Default ResourceManager 创建成功: %v\n", defaultRM != nil)

	fmt.Println("\n=== 所有调度器集成测试完成 ===")
}
