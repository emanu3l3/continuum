package upload

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type mockStore struct {
	InitUploadFunc  func(ctx context.Context, upUUID uuid.UUID, f *FileMetadata) error
	WriteChunkFunc  func(ctx context.Context, upUUID uuid.UUID, chunkID int64, chunkSize int64, chunkBytes []byte) error
	CompleteFunc    func(ctx context.Context, upUUID uuid.UUID) error
	GetMetadataFunc func(ctx context.Context, upUUID uuid.UUID) (*FileMetadata, error)
	GetStateFunc    func(ctx context.Context, upUUID uuid.UUID, extension string) (*UploadState, error)
	CloseFunc       func(upUUID uuid.UUID)
}

func (s *mockStore) InitUpload(ctx context.Context, upUUID uuid.UUID, f *FileMetadata) error {
	if s.InitUploadFunc != nil {
		return s.InitUploadFunc(ctx, upUUID, f)
	}
	return nil
}

func (s *mockStore) WriteChunk(ctx context.Context, upUUID uuid.UUID, chunkID int64, chunkSize int64, chunkBytes []byte) error {
	if s.WriteChunkFunc != nil {
		return s.WriteChunkFunc(ctx, upUUID, chunkID, chunkSize, chunkBytes)
	}
	return nil
}

func (s *mockStore) Complete(ctx context.Context, upUUID uuid.UUID) error {
	if s.CompleteFunc != nil {
		return s.CompleteFunc(ctx, upUUID)
	}
	return nil
}

func (s *mockStore) GetMetadata(ctx context.Context, upUUID uuid.UUID) (*FileMetadata, error) {
	if s.GetMetadataFunc != nil {
		return s.GetMetadataFunc(ctx, upUUID)
	}
	return nil, nil
}

// CORRETTO: Aggiunto il parametro extension per rispecchiare l'interfaccia usata dalla Service
func (s *mockStore) GetState(ctx context.Context, upUUID uuid.UUID, extension string) (*UploadState, error) {
	if s.GetStateFunc != nil {
		return s.GetStateFunc(ctx, upUUID, extension)
	}
	return nil, nil
}

func (s *mockStore) Close(upUUID uuid.UUID) {
	if s.CloseFunc != nil {
		s.CloseFunc(upUUID)
	}
}

type mockRegistry struct {
	GetFunc    func(uuid.UUID) (*UploadContext, error)
	SaveFunc   func(uuid.UUID, *UploadContext) error
	RemoveFunc func(uuid.UUID) error
}

func (r *mockRegistry) Get(upUUID uuid.UUID) (*UploadContext, error) {
	if r.GetFunc != nil {
		return r.GetFunc(upUUID)
	}
	return nil, nil
}

func (r *mockRegistry) Save(upUUID uuid.UUID, upContext *UploadContext) error {
	if r.SaveFunc != nil {
		return r.SaveFunc(upUUID, upContext)
	}
	return nil
}

func (r *mockRegistry) Remove(upUUID uuid.UUID) error {
	if r.RemoveFunc != nil {
		return r.RemoveFunc(upUUID)
	}
	return nil
}

func newMockContext(f *FileMetadata) *UploadContext {
	return &UploadContext{
		FileMtd:       f,
		ChunksWritten: make(map[int64]struct{}),
		Rw:            sync.RWMutex{},
	}
}

// --- Tests ---
var maxInMemSeconds int64 = 10

func TestNewUpload(t *testing.T) {
	errStorage := errors.New("error storage")
	errRegistry := errors.New("error registry")
	f := &FileMetadata{Name: "file", Extension: "pdf", Size: 4294967296, ChunkSize: 2097152, TotalChunks: 2048}

	tests := []struct {
		name        string
		store       *mockStore
		reg         *mockRegistry
		expectedErr error
	}{
		{
			name:        "correct",
			store:       &mockStore{},
			reg:         &mockRegistry{},
			expectedErr: nil,
		},
		{
			name: "storage error",
			store: &mockStore{
				InitUploadFunc: func(ctx context.Context, upUUID uuid.UUID, f *FileMetadata) error {
					return errStorage
				},
			},
			reg:         &mockRegistry{},
			expectedErr: errStorage,
		},
		{
			name:  "registry error",
			store: &mockStore{},
			reg: &mockRegistry{
				SaveFunc: func(u uuid.UUID, uc *UploadContext) error { return errRegistry },
			},
			expectedErr: errRegistry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(tt.store, tt.reg, maxInMemSeconds)
			upUUID, err := s.NewUpload(context.Background(), f)

			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
			if tt.expectedErr == nil && upUUID == uuid.Nil {
				t.Errorf("expected a valid UUID, got Nil")
			}
		})
	}
}

func TestProcessChunk(t *testing.T) {
	errStorage := errors.New("error storage")
	upUUID, _ := uuid.NewV7()
	f := &FileMetadata{Name: "file", Extension: "pdf", Size: 4294967296, ChunkSize: 2097152, TotalChunks: 2048}

	tests := []struct {
		name        string
		chunkID     int64
		store       *mockStore
		reg         *mockRegistry
		expectedErr error
	}{
		{
			name:    "correct data",
			chunkID: 1,
			store:   &mockStore{},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) {
					return newMockContext(f), nil
				},
			},
			expectedErr: nil,
		},
		{
			name:    "upload completed",
			chunkID: 1,
			store:   &mockStore{},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) {
					completedFile := *f
					completedFile.Completed = true
					return newMockContext(&completedFile), nil
				},
			},
			expectedErr: ErrUploadCompleted,
		},
		{
			name:    "wrong chunkID",
			chunkID: f.TotalChunks,
			store:   &mockStore{},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) {
					return newMockContext(f), nil
				},
			},
			expectedErr: ErrChunkID,
		},
		{
			name:    "storage error",
			chunkID: 1,
			store: &mockStore{
				WriteChunkFunc: func(ctx context.Context, upUUID uuid.UUID, chunkID int64, chunkSize int64, chunkBytes []byte) error {
					return errStorage
				},
			},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) {
					return newMockContext(f), nil
				},
			},
			expectedErr: errStorage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(tt.store, tt.reg, maxInMemSeconds)
			err := s.ProcessChunk(context.Background(), strings.NewReader("qwerty"), upUUID, tt.chunkID)

			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
		})
	}
}

func TestProcessStatus(t *testing.T) {
	errRegistry := errors.New("registry error")
	errStorage := errors.New("storage error")
	errNotFound := errors.New("not found in registry")
	upUUID, _ := uuid.NewV7()
	f := &FileMetadata{Name: "file", Extension: "pdf", Size: 4294967296, ChunkSize: 2097152, TotalChunks: 2048}
	upState := &UploadState{}

	tests := []struct {
		name        string
		store       *mockStore
		reg         *mockRegistry
		expectedErr error
	}{
		{
			name:  "correct data (from registry)",
			store: &mockStore{},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) {
					uc := newMockContext(f)
					uc.TotalChunksWritten = 2048
					return uc, nil
				},
			},
			expectedErr: nil,
		},
		{
			name: "storage error (GetMetadata)",
			store: &mockStore{
				GetMetadataFunc: func(ctx context.Context, upUUID uuid.UUID) (*FileMetadata, error) {
					return nil, ErrUploadNotFound
				},
			},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) { return nil, errNotFound },
			},
			expectedErr: ErrUploadNotFound,
		},
		{
			name: "upload completed status",
			store: &mockStore{
				GetMetadataFunc: func(ctx context.Context, upUUID uuid.UUID) (*FileMetadata, error) {
					fCompleted := *f
					fCompleted.Completed = true
					return &fCompleted, nil
				},
			},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) { return nil, errNotFound },
			},
			expectedErr: nil,
		},
		{
			name: "recover chunks error",
			store: &mockStore{
				GetMetadataFunc: func(ctx context.Context, upUUID uuid.UUID) (*FileMetadata, error) { return f, nil },
				GetStateFunc: func(ctx context.Context, upUUID uuid.UUID, extension string) (*UploadState, error) {
					return nil, errStorage
				},
			},
			reg: &mockRegistry{
				GetFunc: func(u uuid.UUID) (*UploadContext, error) { return nil, errNotFound },
			},
			expectedErr: errStorage,
		},
		{
			name: "save registry error",
			store: &mockStore{
				GetMetadataFunc: func(ctx context.Context, upUUID uuid.UUID) (*FileMetadata, error) { return f, nil },
				GetStateFunc: func(ctx context.Context, upUUID uuid.UUID, extension string) (*UploadState, error) {
					return upState, nil
				},
			},
			reg: &mockRegistry{
				GetFunc:  func(u uuid.UUID) (*UploadContext, error) { return nil, errNotFound },
				SaveFunc: func(u uuid.UUID, uc *UploadContext) error { return errRegistry },
			},
			expectedErr: errRegistry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(tt.store, tt.reg, maxInMemSeconds)
			_, err := s.ProcessStatus(context.Background(), upUUID)
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error: %v, got: %v", tt.expectedErr, err)
			}
		})
	}
}
