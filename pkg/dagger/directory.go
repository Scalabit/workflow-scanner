package dagger

//go:generate mockgen -source=directory.go -destination=../../mocks/directory_mock.go -package=mocks Directory

import "workflow-scanner/internal/dagger"

type Directory interface {
	File(path string) *dagger.File
}

type DirectoryImpl struct {
	internal *dagger.Directory
}

func (directoryImpl *DirectoryImpl) File(path string) *dagger.File {
	return directoryImpl.internal.File(path)
}
