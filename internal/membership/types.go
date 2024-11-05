package membership

import (
	"context"
	"time"

	"go.etcd.io/raft/v3"
	etcdraftpb "go.etcd.io/raft/v3/raftpb"

	"github.com/shaj13/raft/internal/raftpb"
	"github.com/shaj13/raft/internal/transport"
	"github.com/shaj13/raft/raftlog"
)

// Member represents a raft cluster member.
type Member interface {
	ID() uint64
	Address() string
	ActiveSince() time.Time
	IsActive() bool
	Update(m raftpb.Member) error
	Send(etcdraftpb.Message) error
	Type() raftpb.MemberType
	Raw() raftpb.Member
	Close() error
	TearDown(ctx context.Context) error
}

// Reporter is used to report on a member status.
type Reporter interface {
	ReportUnreachable(id uint64)
	ReportShutdown(id uint64)
	ReportSnapshot(id uint64, status raft.SnapshotStatus)
}

// Config define common configuration used by the pool.
type Config interface {
	StreamTimeout() time.Duration
	DrainTimeout() time.Duration
	Reporter() Reporter
	Logger() raftlog.Logger
	Dial() transport.Dial
	AllowPipelining() bool
}

// Pool represents a set of raft Members.
type Pool interface {
	NextID(ctx context.Context) uint64
	Members() []Member
	Get(context.Context, uint64) (Member, bool)
	Add(context.Context, raftpb.Member) error
	Update(context.Context, raftpb.Member) error
	Remove(context.Context, raftpb.Member) error
	Snapshot(context.Context) []raftpb.Member
	Restore(context.Context, []raftpb.Member)
	RegisterTypeMatcher(func(raftpb.Member) raftpb.MemberType)
	TearDown(context.Context) error
}
