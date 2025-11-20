package dagger

//go:generate mockgen -source=currentModule.go -destination=../../mocks/currentModule_mock.go -package=mocks CurrentModule

import "workflow-scanner/internal/dagger"

type CurrentModule interface {
	Source() Directory
}

type CurrentModuleImpl struct {
	internal *dagger.CurrentModule
}

func (currentModuleImpl *CurrentModuleImpl) Source() Directory {
	return &DirectoryImpl{internal: currentModuleImpl.internal.Source()}
}
