package models

type NodeClientInterface interface {
	NeedsUpdate() (bool, error)
	TerminateNode() error
}
