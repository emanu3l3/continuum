package diskstorage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/emanu3l3/continuum/pkg/upload"
	"github.com/google/uuid"
)

func TestInitUpload(t *testing.T) {
	baseDir := t.TempDir()
	store := NewDiskStorage(baseDir)

	upUUID, _ := uuid.NewV7()
	fMtd := &upload.FileMetadata{
		Name:      "test",
		Extension: "pdf",
	}

	err := store.InitUpload(context.Background(), upUUID, fMtd)
	if err != nil {
		t.Fatalf("InitUpload failed: %v", err)
	}

	upFiles := store.activeUploads[upUUID]

	defer func() {
		upFiles.file.Close()
		upFiles.fileMetadata.Close()
		upFiles.fileState.Close()

	}()

	pathDir := filepath.Join(baseDir, upUUID.String())
	if _, err := os.Stat(pathDir); os.IsNotExist(err) {
		t.Errorf("folder not created")
	}

	metaPath := filepath.Join(pathDir, upUUID.String()+".json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("can't read metadata file: %v", err)
	}

	var readMtd upload.FileMetadata
	json.Unmarshal(metaBytes, &readMtd)
	if readMtd.Name != "test" || readMtd.Extension != "pdf" {
		t.Errorf("wrong metadata")
	}
}

func TestWriteChunk(t *testing.T) {
	baseDir := t.TempDir()
	store := NewDiskStorage(baseDir)
	upUUID, _ := uuid.NewV7()
	fMtd := &upload.FileMetadata{
		Name:      "test",
		Extension: "pdf",
		ChunkSize: 10,
	}

	store.InitUpload(context.Background(), upUUID, fMtd)

	upFiles := store.activeUploads[upUUID]
	defer upFiles.file.Close()
	defer upFiles.fileMetadata.Close()
	defer upFiles.fileState.Close()

	var chunkID int64 = 0
	chunkData := []byte("helloworld") // 10 bytes

	err := store.WriteChunk(context.Background(), upUUID, chunkID, fMtd.ChunkSize, chunkData)
	if err != nil {
		t.Fatalf("chunk writing failed: %v", err)
	}
}
func TestComplete(t *testing.T) {
	baseDir := t.TempDir()
	store := NewDiskStorage(baseDir)

	upUUID, _ := uuid.NewV7()
	fMtd := &upload.FileMetadata{
		Name:      "test",
		Extension: "pdf",
		ChunkSize: 10,
	}
	store.InitUpload(context.Background(), upUUID, fMtd)

	upFiles := store.activeUploads[upUUID]

	_ = json.NewEncoder(upFiles.fileMetadata).Encode(fMtd)

	err := store.Complete(context.Background(), upUUID)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	metaBytes, err := os.ReadFile(upFiles.fileMetadata.Name())
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}

	var finalMtd upload.FileMetadata
	_ = json.Unmarshal(metaBytes, &finalMtd)

	if !finalMtd.Completed {
		t.Errorf("metadata file has not been updated")
	}

	_, writeErr := upFiles.file.Write([]byte("test"))
	if writeErr == nil {
		t.Errorf("expected FilePointer to be closed")
	}
}
func TestGetMetadata(t *testing.T) {
	baseDir := t.TempDir()
	store := NewDiskStorage(baseDir)
	upUUID, _ := uuid.NewV7()
	fMtd := &upload.FileMetadata{
		Name:      "test",
		Extension: "pdf",
		ChunkSize: 10,
		Completed: true,
	}
	store.InitUpload(context.Background(), upUUID, fMtd)
	upFiles := store.activeUploads[upUUID]

	defer func() {
		upFiles.file.Close()
		upFiles.fileMetadata.Close()
		upFiles.fileState.Close()
	}()

	// test the retrieval
	parsed, err := store.GetMetadata(context.Background(), upUUID)
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}

	if parsed.Name != "test" || !parsed.Completed {
		t.Errorf("extracted metadata does not match the saved one")
	}
}

func TestRecoverChunksWritten(t *testing.T) {
	baseDir := t.TempDir()
	store := NewDiskStorage(baseDir)
	upUUID, _ := uuid.NewV7()
	fMtd := &upload.FileMetadata{
		Name:      "test",
		Extension: "pdf",
		ChunkSize: 10,
	}
	store.InitUpload(context.Background(), upUUID, fMtd)

	upFiles := store.activeUploads[upUUID]
	defer func() {
		upFiles.file.Close()
		upFiles.fileMetadata.Close()
		upFiles.fileState.Close()
	}()

	// simulate a state file with chunks 0, 2, and 5 already written (note the empty lines)
	upFiles.fileState.WriteString("0\n2\n\n5\n")

	upState, err := store.GetState(context.Background(), upUUID, fMtd.Extension)
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	if upState.TotalChunksWritten != 3 {
		t.Errorf("expected 3 total chunks, got: %d", upState.TotalChunksWritten)
	}

	if len(upState.ChunksWritten) != 3 {
		t.Errorf("expected 3 items in the map, found: %d", len(upState.ChunksWritten))
	}

	// check for the specific presence of skipped IDs (2 and 5)
	if _, ok := upState.ChunksWritten[5]; !ok {
		t.Errorf("Chunk ID 5 not recovered")
	}
}
