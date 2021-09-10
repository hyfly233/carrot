package client

import "flag"

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
}
