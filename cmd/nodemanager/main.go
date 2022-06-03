package main

import (
	"carrot/internal/common"
	"carrot/internal/nodemanager"
	"flag"
	"log"
)

func main() {
	var (
		port   = flag.Int("port", 8042, "NodeManager port")
		host   = flag.String("host", "localhost", "NodeManager host")
		rmURL  = flag.String("rm-url", "http://localhost:8088", "ResourceManager URL")
		memory = flag.Int64("memory", 8192, "Total memory in MB")
		vcores = flag.Int("vcores", 8, "Total virtual cores")
	)
	flag.Parse()

	log.Printf("Starting YARN NodeManager on %s:%d...", *host, *port)

	nodeID := common.NodeID{
		Host: *host,
		Port: int32(*port),
	}

	totalResource := common.Resource{
		Memory: *memory,
		VCores: int32(*vcores),
	}

	nm := nodemanager.NewNodeManager(nodeID, totalResource, *rmURL)

	if err := nm.Start(*port); err != nil {
		log.Fatalf("Failed to start NodeManager: %v", err)
	}
}
