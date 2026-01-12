package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path"

	"kerrareg/pkg/storage/types"
)

type FileSystem struct{}

// fileExists determines whether a file or directory exists on the provided path.
func (storage *FileSystem) fileExists(filename string) (bool, error) {
	_, err := os.Stat(filename)
	if err == nil {
		return true, err
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	return false, err
}

// GetObjectChecksum determines if the file exists and sets the soi receiver's field 'FileNotExists' to true if the file cannot be found.
// When the file is found the function sets the soi receiver's field 'ObjectChecksum' with the base64 encoded sha256 checksum of the file.
func (storage *FileSystem) GetObjectChecksum(ctx context.Context, soi *types.StorageObjectInput) error {
	fileExists, err := storage.fileExists(*soi.FilePath)
	if err != nil {
		soi.FileNotExists = true
		return err
	}

	if !fileExists {
		soi.FileNotExists = true
		return err
	}

	fileBytes, err := os.ReadFile(*soi.FilePath)
	if err != nil {
		return err
	}

	sha256Sum := sha256.Sum256(fileBytes)
	checksumSha256 := base64.StdEncoding.EncodeToString(sha256Sum[:])
	soi.ObjectChecksum = &checksumSha256
	return nil
}

// DeleteObject removes the object received by soi from the filesystem.
func (storage *FileSystem) DeleteObject(ctx context.Context, soi *types.StorageObjectInput) error {
	if err := os.Remove(*soi.FilePath); err != nil {
		return err
	}

	return nil
}

// PutObject puts the Version file in the directory specified by StorageConfig.FileSystem.DirectoryPath. If a directory
// for the Module's name is not found the function will create it first.
func (storage *FileSystem) PutObject(ctx context.Context, soi *types.StorageObjectInput) error {
	dir, _ := path.Split(*soi.FilePath)
	exists, err := storage.fileExists(dir)
	if err != nil {
		return err
	}

	if !exists {
		if err := os.Mkdir(dir, os.FileMode(0644)); err != nil {
			return err
		}
	}

	permissions := os.FileMode(0644)
	if err := os.WriteFile(
		*soi.FilePath,
		soi.FileBytes,
		permissions,
	); err != nil {
		return err
	}

	return nil
}
