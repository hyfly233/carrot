package client

import (
	"carrot/internal/common"
	"flag"
	"log"
)

func main() {
	var (
		rmURL   = flag.String("rm-url", "http://localhost:8088", "ResourceManager URL")
		appName = flag.String("app-name", "test-app", "Application name")
		appType = flag.String("app-type", "YARN", "Application type")
		queue   = flag.String("queue", "default", "Queue name")
		memory  = flag.Int64("memory", 1024, "AM memory in MB")
		vcores  = flag.Int("vcores", 1, "AM virtual cores")
		command = flag.String("command", "sleep 60", "Command to run")
	)
	flag.Parse()

	log.Printf("Submitting application to YARN...")

	// 获取新的 Application ID
	appID, err := getNewApplicationID(*rmURL)
	if err != nil {
		log.Fatalf("Failed to get new application ID: %v", err)
	}

	log.Printf("Got application ID: %+v", appID)

	// 准备应用程序提交上下文
	submitCtx := common.ApplicationSubmissionContext{
		ApplicationID:   *appID,
		ApplicationName: *appName,
		ApplicationType: *appType,
		Queue:           *queue,
		Priority:        1,
		Resource: common.Resource{
			Memory: *memory,
			VCores: int32(*vcores),
		},
		AMContainerSpec: common.ContainerLaunchContext{
			Commands: []string{*command},
			Environment: map[string]string{
				"JAVA_HOME": "/usr/lib/jvm/java-8-openjdk-amd64",
			},
		},
		MaxAppAttempts: 2,
	}

	// 提交应用程序
	if err := submitApplication(*rmURL, submitCtx); err != nil {
		log.Fatalf("Failed to submit application: %v", err)
	}

	log.Printf("Application %s submitted successfully", *appName)

	// 监控应用程序状态
	monitorApplication(*rmURL, appID)
}
