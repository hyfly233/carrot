package resourcemanager

import (
	"flag"
	"log"

	"carrot/internal/resourcemanager"
)

func main() {
	var (
		port = flag.Int("port", 8088, "ResourceManager port")
	)
	flag.Parse()

	log.Println("Starting YARN ResourceManager...")

	rm := resourcemanager.NewResourceManager()

	if err := rm.Start(*port); err != nil {
		log.Fatalf("Failed to start ResourceManager: %v", err)
	}
}
