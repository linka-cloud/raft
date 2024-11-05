package storage

import (
	"context"
	"io"

	"go.etcd.io/raft/v3"
	etcdraftpb "go.etcd.io/raft/v3/raftpb"

	"github.com/shaj13/raft/internal/raftpb"
)

// Snapshot is the state of a system at a particular point in time.
type Snapshot struct {
	raftpb.SnapshotState
	Data io.ReadCloser
}

// Snapshotter define a set of functions to read and write snapshots.
type Snapshotter interface {
	Writer(uint64, uint64) (io.WriteCloser, error)
	Reader(uint64, uint64) (io.ReadCloser, error)
	Write(*Snapshot) error
	Read(uint64, uint64) (*Snapshot, error)
	ReadFrom(string) (*Snapshot, error)
}

// Storage define a set of functions to persist raft data,
// To provide durability and ensure data integrity.
type Storage interface {
	raft.Storage
	CreateSnapshot(ctx context.Context, i uint64, cs *etcdraftpb.ConfState, data []byte) (etcdraftpb.Snapshot, error)
	ApplySnapshot(snap etcdraftpb.Snapshot) error
	Compact(compactIndex uint64) error

	SaveSnapshot(context.Context, *etcdraftpb.Snapshot) error
	SaveEntries(context.Context, *etcdraftpb.HardState, []etcdraftpb.Entry) error
	Snapshotter() Snapshotter
	Boot([]byte) ([]byte, *etcdraftpb.HardState, []etcdraftpb.Entry, *Snapshot, error)
	Exist() bool
	Close() error
}
