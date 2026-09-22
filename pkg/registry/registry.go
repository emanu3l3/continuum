package registry

import (
	"errors"
	"sync"

	"github.com/emanu3l3/continuum/pkg/upload"
	"github.com/google/uuid"
)

var (
	ErrUploadNotFound = errors.New("couldn't retrieve the upload.")
	ErrAlreadySaved   = errors.New("upload already saved.")
)

type registry struct {
	rw      sync.RWMutex
	uploads map[uuid.UUID]*upload.UploadContext
}

func NewRegistry() *registry {
	return &registry{
		uploads: make(map[uuid.UUID]*upload.UploadContext),
	}
}

func (u *registry) Get(upUUID uuid.UUID) (*upload.UploadContext, error) {
	u.rw.RLock()
	upContext, exists := u.uploads[upUUID]
	u.rw.RUnlock()

	if exists {
		return upContext, nil
	}

	return nil, ErrUploadNotFound

}

func (u *registry) Save(upUUID uuid.UUID, upContext *upload.UploadContext) error {
	u.rw.Lock()
	defer u.rw.Unlock()

	if _, exists := u.uploads[upUUID]; exists {
		return ErrAlreadySaved
	}

	u.uploads[upUUID] = upContext
	return nil
}

func (u *registry) Remove(upUUID uuid.UUID) error {
	u.rw.Lock()
	defer u.rw.Unlock()

	upContext, exists := u.uploads[upUUID]
	if !exists {
		return ErrUploadNotFound
	}
	upContext.InactivityTimer.Stop()

	delete(u.uploads, upUUID)

	return nil
}
