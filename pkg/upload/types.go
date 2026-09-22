package upload

import (
	"context"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const upUUIDKey contextKey = "upUUID"

type FileMetadata struct {
	Name        string    `json:"name"`         // file name
	Extension   string    `json:"extension"`    // file extension (pdf, mp4, png, ...)
	Size        int64     `json:"size"`         // size of the file in bytes
	ChunkSize   int64     `json:"chunk_size"`   // size of each chunk in bytes
	TotalChunks int64     `json:"total_chunks"` // total number of chunks for the upload
	Completed   bool      `json:"completed"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// UploadStatus represents the current state of an upload.
// If the upload is completed, ChunksWritten will be an empty array.
type UploadStatus struct {
	FileMtd       *FileMetadata `json:"file"`
	ChunksWritten []int         `json:"chunks_written"`
}

// UploadState is used as a DTO in the storage.
type UploadState struct {
	ChunksWritten      map[int64]struct{}
	TotalChunksWritten int64
}

func NewUploadStatus(upContext *UploadContext) *UploadStatus {
	chunks := []int{}
	if !upContext.FileMtd.Completed {
		chunks = make([]int, 0, len(upContext.ChunksWritten))
		for key := range upContext.ChunksWritten {
			chunks = append(chunks, int(key))
		}

		slices.Sort(chunks)
	}

	return &UploadStatus{
		FileMtd:       upContext.FileMtd,
		ChunksWritten: chunks,
	}
}

type Service interface {
	// NewUpload generates a new UUID, initializes the upload and register
	// the upload state in the UploadRegistry.
	NewUpload(ctx context.Context, f *FileMetadata) (uuid.UUID, error)

	// ProcessChunk verifies if the upload is not completed and that the chunk
	// has not been written yet, then delegates the write to storage.
	ProcessChunk(ctx context.Context, body io.Reader, upUUID uuid.UUID, chunkID int64) error

	// ProcessStatus retrieves current status for an upload and restores its state
	// into the UploadRegistry only if it wasn't completed yet.
	ProcessStatus(ctx context.Context, upUUID uuid.UUID) (*UploadStatus, error)
}

// UploadContext holds the in-memory state of an in-progress, or resumed,
// upload. It's tracked by the UploadRegistry, and mutated concurrently by ProcessChunk calls
// so access to its mutable fields must be guarded by Rw.
type UploadContext struct {
	Rw                 sync.RWMutex
	InactivityTimer    *time.Timer // resets on each chunk write; fires if the upload is not concluded
	FileMtd            *FileMetadata
	ChunksWritten      map[int64]struct{} // set containing the chunkID of the chunks that have been written
	TotalChunksWritten int64              // counter of the chunks that have been written
}

func NewUploadContext(f *FileMetadata) *UploadContext {
	upContext := UploadContext{
		FileMtd:       f,
		ChunksWritten: make(map[int64]struct{}),
	}

	return &upContext
}

type Storage interface {
	// InitUpload initializes the upload and saves initial metadata.
	InitUpload(ctx context.Context, upUUID uuid.UUID, f *FileMetadata) error

	// WriteChunk writes the chunk data to the file at the offset: chunkID * chunkSize.
	WriteChunk(ctx context.Context, upUUID uuid.UUID, chunkID int64, chunkSize int64, chunkchunkBytes []byte) error

	// Complete set the upload as completed, upadates metadata and closes the resources.
	Complete(ctx context.Context, upUUID uuid.UUID) error

	// GetMetadata returns a pointer to a FileMetadata struct containing the metadata information.
	GetMetadata(ctx context.Context, upUUID uuid.UUID) (*FileMetadata, error)

	// GetState retrieves the state of the upload, which and how many chunks have been written.
	GetState(ctx context.Context, upUUID uuid.UUID, ext string) (*UploadState, error)

	// Close closes/free all upload resources
	Close(upUUID uuid.UUID)
}

type UploadRegistry interface {
	// Get returns the UploadContext associated with the given upUUID.
	// It returns an error if there's no uploads associated with that upUUID.
	Get(upUUID uuid.UUID) (*UploadContext, error)

	// Save stores the upContext under the given upUUID.
	// It returns an error if it's already registered.
	Save(upUUID uuid.UUID, upContext *UploadContext) error

	// Remove deletes the UploadContext associated with the given upUUID from the registry.
	// It returns an error if no upload is registered under that upUUID.
	Remove(uuid.UUID) error
}
