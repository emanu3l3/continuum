package diskstorage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/emanu3l3/continuum/pkg/upload"
	"github.com/google/uuid"
)

const filePerm = 0644
const dirPerm = 0755

var ErrChunkAlreadyWritten = errors.New("chunk already written.")

type uploadFiles struct {
	file         *os.File
	fileMetadata *os.File
	fileState    *os.File
}

type diskStorage struct {
	rw            sync.RWMutex
	baseFilePath  string
	activeUploads map[uuid.UUID]*uploadFiles
}

func NewDiskStorage(baseFilePath string) *diskStorage {
	return &diskStorage{
		baseFilePath:  baseFilePath,
		activeUploads: make(map[uuid.UUID]*uploadFiles),
	}
}

// openFilesAndRegister opens and saves the files associated with the upUUID in the activeUploads map
func (s *diskStorage) openFilesAndRegister(upUUID uuid.UUID, extension string) (*uploadFiles, error) {
	strUUID := upUUID.String()
	pathDir := filepath.Join(s.baseFilePath, strUUID)

	filePath := filepath.Join(pathDir, strUUID+"."+extension)
	filePathMetadata := filepath.Join(pathDir, strUUID+".json")
	filePathState := filepath.Join(pathDir, strUUID+".state")

	filePointer, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return nil, err
	}

	fileMetadataPointer, err := os.OpenFile(filePathMetadata, os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		filePointer.Close()
		return nil, err
	}

	fileStatePointer, err := os.OpenFile(filePathState, os.O_CREATE|os.O_RDWR|os.O_APPEND, filePerm)
	if err != nil {
		filePointer.Close()
		fileMetadataPointer.Close()
		return nil, err
	}

	upFiles := &uploadFiles{
		file:         filePointer,
		fileMetadata: fileMetadataPointer,
		fileState:    fileStatePointer,
	}

	s.rw.Lock()
	s.activeUploads[upUUID] = upFiles
	s.rw.Unlock()

	return upFiles, nil
}

// getUploadFiles retrieves from the activeUploads map the files associated with the upUUID
func (s *diskStorage) getUploadFiles(upUUID uuid.UUID) (*uploadFiles, error) {
	s.rw.RLock()
	upFiles, exists := s.activeUploads[upUUID]
	s.rw.RUnlock()

	if exists {
		return upFiles, nil
	}

	return nil, errors.New("couldn't recover files.")
}

func (s *diskStorage) InitUpload(ctx context.Context, upUUID uuid.UUID, file *upload.FileMetadata) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	pathDir := filepath.Join(s.baseFilePath, upUUID.String())
	err := os.MkdirAll(pathDir, dirPerm)
	if err != nil {
		return err
	}

	upFiles, err := s.openFilesAndRegister(upUUID, file.Extension)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(upFiles.fileMetadata)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(file)
	if err != nil {
		s.Close(upUUID)
		return err
	}

	return nil
}

func (s *diskStorage) WriteChunk(ctx context.Context, upUUID uuid.UUID, chunkID int64, chunkSize int64, chunkBytes []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	upFiles, err := s.getUploadFiles(upUUID)
	if err != nil {
		return err
	}

	offset := chunkID * chunkSize
	_, err = upFiles.file.WriteAt(chunkBytes, offset)
	if err != nil {
		return err
	}

	// write the chunkID in the state file
	s.rw.Lock()
	defer s.rw.Unlock()

	_, err = fmt.Fprintf(upFiles.fileState, "%d\n", chunkID)
	if err != nil {
		return err
	}

	return nil
}

func (s *diskStorage) Complete(ctx context.Context, upUUID uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	upFiles, err := s.getUploadFiles(upUUID)
	if err != nil {
		return err
	}

	if _, err := upFiles.fileMetadata.Seek(0, 0); err != nil {
		return err
	}

	var fileMtd upload.FileMetadata
	if err := json.NewDecoder(upFiles.fileMetadata).Decode(&fileMtd); err != nil {
		return err
	}

	fileMtd.Completed = true
	fileMtd.CompletedAt = time.Now().UTC()

	filePath := upFiles.fileMetadata.Name()
	dir := filepath.Dir(filePath)

	pattern := upUUID.String() + "_*json"
	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()

	success := false
	defer func() {
		tmpFile.Close()
		if !success {
			os.Remove(tmpName)
		}
	}()

	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(fileMtd); err != nil {
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := upFiles.fileMetadata.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, filePath); err != nil {
		return err
	}

	// close all files and remove the upload from the activeUploads map
	success = true
	s.Close(upUUID)

	// delete state file
	err = os.Remove(upFiles.fileState.Name())
	if err != nil {
		return err
	}

	return nil
}

func (s *diskStorage) GetMetadata(ctx context.Context, upUUID uuid.UUID) (*upload.FileMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	strUUID := upUUID.String()
	pathDir := filepath.Join(s.baseFilePath, strUUID)
	filePathMetadata := filepath.Join(pathDir, strUUID+".json")

	data, err := os.ReadFile(filePathMetadata)
	if err != nil {
		return nil, err
	}

	var fileMtd upload.FileMetadata
	if err = json.Unmarshal(data, &fileMtd); err != nil {
		return nil, err
	}

	return &fileMtd, nil
}

func (s *diskStorage) GetState(ctx context.Context, upUUID uuid.UUID, extension string) (*upload.UploadState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	upFiles, err := s.getUploadFiles(upUUID)
	if err != nil {
		upFiles, err = s.openFilesAndRegister(upUUID, extension)
		if err != nil {
			return nil, err
		}
	}

	upState := &upload.UploadState{
		ChunksWritten: make(map[int64]struct{}),
	}

	if _, err := upFiles.fileState.Seek(0, 0); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(upFiles.fileState)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		chunkID, _ := strconv.ParseInt(scanner.Text(), 10, 64)
		upState.ChunksWritten[chunkID] = struct{}{}
		upState.TotalChunksWritten += 1
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return upState, nil
}

func (s *diskStorage) Close(upUUID uuid.UUID) {
	upFiles, err := s.getUploadFiles(upUUID)
	if err == nil {
		_ = upFiles.file.Close()
		_ = upFiles.fileMetadata.Close()
		_ = upFiles.fileState.Close()

		delete(s.activeUploads, upUUID)
	}
}
