package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Printf("no .env found")
		return
	}

	maxFileSizeMB, err := strconv.ParseInt(os.Getenv("MAX_FILE_SIZE_MB"), 10, 64)
	if err != nil {
		log.Fatal(err)
	}

	maxChunkSizeMB, err := strconv.ParseInt(os.Getenv("MAX_CHUNK_SIZE_MB"), 10, 64)
	if err != nil {
		log.Fatal(err)
	}

	if maxChunkSizeMB > maxFileSizeMB || maxChunkSizeMB == 0 || maxFileSizeMB == 0 {
		log.Fatal("file and chunk size are not properly set.")
	}

	maxInMemSeconds, err := strconv.ParseInt(os.Getenv("MAX_IN_MEMORY_SECONDS"), 10, 64)
	if err != nil {
		log.Fatal(err)
	}

	if maxInMemSeconds <= 0 {
		log.Fatal("invaild MAX_IN_MEMORY_SECONDS value")
	}

	maxFileSize := maxFileSizeMB * 1_024 * 1_024
	maxChunkSize := maxChunkSizeMB * 1_024 * 1_024

	var acceptedExtensions = map[string]struct{}{
		"pdf":  {},
		"mp4":  {},
		"png":  {},
		"jpg":  {},
		"jpeg": {},
		"mov":  {},
	}

	cfg := config{
		addr:               os.Getenv("ADDRESS"),
		baseFilePath:       filepath.Clean(os.Getenv("BASE_FILE_PATH")),
		maxFileSize:        maxFileSize,
		maxChunkSize:       maxChunkSize,
		maxInMemSeconds:    maxInMemSeconds,
		acceptedExtensions: acceptedExtensions,
	}

	api := application{
		cfg: cfg,
	}

	if err := api.run(api.mount()); err != nil {
		log.Fatal(err)
	}
}
