package csi

import (
	"context"
	"errors"
)

var ErrMissingFields = errors.New("missing required fields")

type VolumeDevice struct {
	VolumeHandle string
	Driver       string
	Device       string
	Node         string
	PVName       string

	PVCName   string
	Namespace string
}

type Discoverer interface {
	Name() string
	Discover(ctx context.Context) ([]VolumeDevice, error)
}
