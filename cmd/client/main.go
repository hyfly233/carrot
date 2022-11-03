package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"

	"carrot/internal/common"
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

func getNewApplicationID(rmURL string) (*common.ApplicationID, error) {
	resp, err := http.Post(rmURL+"/ws/v1/cluster/apps/new-application", "application/json", nil)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatalf("Failed to close response body: %v", err)
		}
	}(resp.Body)

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	appIDData := result["application-id"].(map[string]interface{})
	appID := &common.ApplicationID{
		ClusterTimestamp: int64(appIDData["cluster_timestamp"].(float64)),
		ID:               int32(appIDData["id"].(float64)),
	}

	return appID, nil
}

func submitApplication(rmURL string, ctx common.ApplicationSubmissionContext) error {
	jsonData, err := json.Marshal(ctx)
	if err != nil {
		return err
	}

	resp, err := http.Post(rmURL+"/ws/v1/cluster/apps", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatalf("Failed to close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("submit failed with status: %d", resp.StatusCode)
	}

	return nil
}

func monitorApplication(rmURL string, appID *common.ApplicationID) {
	log.Printf("Monitoring application status...")

	// 简单的状态查询，实际应该定期查询
	resp, err := http.Get(rmURL + "/ws/v1/cluster/apps")
	if err != nil {
		log.Printf("Failed to get applications: %v", err)
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatalf("Failed to close response body: %v", err)
		}
	}(resp.Body)

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Failed to decode response: %v", err)
		return
	}

	apps := result["apps"].(map[string]interface{})["app"].([]interface{})
	for _, appData := range apps {
		app := appData.(map[string]interface{})
		appIDMap := app["application_id"].(map[string]interface{})

		if int64(appIDMap["cluster_timestamp"].(float64)) == appID.ClusterTimestamp &&
			int32(appIDMap["id"].(float64)) == appID.ID {
			log.Printf("Application state: %s", app["state"])
			log.Printf("Application progress: %.2f%%", app["progress"].(float64)*100)
			break
		}
	}
}
