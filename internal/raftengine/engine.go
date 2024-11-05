package raftengine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime/trace"
	"sync"
	"time"

	"go.etcd.io/etcd/pkg/v3/idutil"
	"go.etcd.io/raft/v3"
	etcdraftpb "go.etcd.io/raft/v3/raftpb"

	"github.com/shaj13/raft/internal/atomic"
	"github.com/shaj13/raft/internal/membership"
	"github.com/shaj13/raft/internal/msgbus"
	"github.com/shaj13/raft/internal/raftpb"
	"github.com/shaj13/raft/internal/storage"
	"github.com/shaj13/raft/raftlog"
)

var (
	// ErrStopped is returned by the Engine methods after a call to
	// Shutdown or when it has not started.
	ErrStopped = errors.New("raft: node not ready yet or has been stopped")
	// ErrNoLeader is returned by the Engine methods when leader lost, or
	// no elected cluster leader.
	ErrNoLeader = errors.New("raft: no elected cluster leader")
	// ErrAlreadySnapshotting can be returned by the StateMachine.Snapshot method
	// or the ForceSnapshot method to indicate that a snapshot is already in progress.
	ErrAlreadySnapshotting = errors.New("raft: already snapshotting")
	// ErrFailedPrecondition can be returned by the StateMachine.Snapshot method
	// to indicate that the precondition for creating a snapshot is not met.
	ErrFailedPrecondition = errors.New("raft: precondition failed")
)

// Engine represents the underlying raft node processor.
type Engine interface {
	LinearizableRead(ctx context.Context) error
	Push(m etcdraftpb.Message) error
	TransferLeadership(context.Context, uint64) error
	Status() (raft.Status, error)
	Shutdown(context.Context) error
	ProposeReplicate(ctx context.Context, data []byte) error
	ProposeConfChange(ctx context.Context, m *raftpb.Member, t etcdraftpb.ConfChangeType) error
	ForgetLeader(ctx context.Context) error
	CreateSnapshot() (etcdraftpb.Snapshot, error)
	Start(ctx context.Context, addr string, oprs ...Operator) error
	ReportUnreachable(id uint64)
	ReportSnapshot(id uint64, status raft.SnapshotStatus)
	ReportShutdown(id uint64)
}

// New construct and return new engine from the provided config.
func New(cfg Config) Engine {
	d := &engine{}
	d.cfg = cfg
	d.fsm = d.cfg.StateMachine()
	d.storage = cfg.Storage()
	d.msgbus = msgbus.New()
	d.pool = cfg.Pool()
	d.started = atomic.NewBool()
	d.appliedIndex = atomic.NewUint64()
	d.snapIndex = atomic.NewUint64()
	d.snapshoting = atomic.NewBool()
	d.logger = cfg.Logger()
	d.statec = cfg.StateChangeCh()
	return d
}

type engine struct {
	ctx    context.Context
	cancel context.CancelFunc
	fsm    StateMachine
	local  *raftpb.Member
	cfg    Config
	node   raft.Node
	// wg waits for most of the engine goroutines to be terminated before
	// shutting down the node.
	wg sync.WaitGroup
	// propwg waits for all the proposals to be terminated before
	// shutting down the node.
	propwg sync.WaitGroup
	// processwg waits for all the process goroutines to be terminated before
	// shutting down the node.
	processwg sync.WaitGroup
	// cache     *raft.MemoryStorage
	// cache *raftwal.DiskStorage
	storage storage.Storage
	// storage      *raftwal.DiskStorage
	msgbus       *msgbus.MsgBus
	idgen        *idutil.Generator
	pool         membership.Pool
	started      *atomic.Bool
	snapIndex    *atomic.Uint64
	snapshoting  *atomic.Bool
	appliedIndex *atomic.Uint64
	proposec     chan etcdraftpb.Message
	msgc         chan etcdraftpb.Message
	snapshotc    chan chan error
	confState    *etcdraftpb.ConfState
	logger       raftlog.Logger
	statec       chan raft.StateType
	appendc      chan etcdraftpb.Message
	applyc       chan etcdraftpb.Message
	leader       bool
}

func (eng *engine) LinearizableRead(ctx context.Context) error {
	if eng.started.False() {
		return ErrStopped
	}

	eng.propwg.Add(1)
	defer eng.propwg.Done()

	// read raft leader index.
	index, err := func() (uint64, error) {
		dur := eng.cfg.TickInterval() * 5
		buf := make([]byte, 8)
		id := eng.idgen.Next()
		binary.BigEndian.PutUint64(buf, id)
		sub := eng.msgbus.SubscribeOnce(id)
		t := time.NewTicker(dur)

		defer t.Stop()
		defer sub.Unsubscribe()

		for {
			err := eng.node.ReadIndex(ctx, buf)
			if err != nil {
				return 0, err
			}

			select {
			case <-t.C:
			case v := <-sub.Chan():
				if err, ok := v.(error); ok {
					return 0, err
				}
				return v.(uint64), nil
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-eng.ctx.Done():
				return 0, ErrStopped
			}
		}
	}()

	if err != nil {
		return err
	}

	// current node is up to date.
	if index <= eng.appliedIndex.Get() {
		return nil
	}

	// wait until leader index applied into this node.
	return eng.wait(ctx, index)
}

// ReportUnreachable reports the given node is not reachable for the last send.
func (eng *engine) ReportUnreachable(id uint64) {
	if eng.started.False() {
		return
	}

	eng.node.ReportUnreachable(id)
}

func (eng *engine) ReportSnapshot(id uint64, status raft.SnapshotStatus) {
	if eng.started.False() {
		return
	}

	eng.node.ReportSnapshot(id, status)
}

func (eng *engine) ReportShutdown(id uint64) {
	if eng.started.False() {
		return
	}

	eng.logger.Info("raft.engine: this member removed from the cluster! shutting down.")

	ctx, cancel := context.WithTimeout(context.Background(), eng.cfg.DrainTimeout())
	defer cancel()

	if err := eng.Shutdown(ctx); err != nil {
		eng.logger.Fatal(err)
	}
}

// Push msg to the engine queue.
func (eng *engine) Push(msg etcdraftpb.Message) error {
	if eng.started.False() {
		return ErrStopped
	}

	eng.propwg.Add(1)
	defer eng.propwg.Done()

	if err := eng.ctx.Err(); err != nil {
		return err
	}

	// chan based on msg type.
	c := eng.msgc
	if msg.Type == etcdraftpb.MsgProp {
		c = eng.proposec
	}

	select {
	case c <- msg:
	case <-eng.ctx.Done():
		return eng.ctx.Err()
	default:
		return errors.New("buffer is full (overloaded network)")
	}

	return nil
}

// Status returns the current status of the raft state machine.
func (eng *engine) Status() (raft.Status, error) {
	if eng.started.False() {
		return raft.Status{}, ErrStopped
	}

	return eng.node.Status(), nil
}

// Close the engine.
func (eng *engine) Shutdown(ctx context.Context) error {
	if eng.started.False() {
		return ErrStopped
	}

	eng.started.UnSet()

	// spawn a goroutine to force shutdown when the provided context
	// expires before the graceful shutdown is complete.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func(ctx context.Context, cancel context.CancelFunc) {
		<-ctx.Done()
		cancel()
	}(ctx, eng.cancel)

	fns := []func() error{
		nopClose(eng.propwg.Wait),
		nopClose(func() {
			close(eng.proposec)
			close(eng.msgc)
			eng.processwg.Wait()
		}),
		nopClose(eng.cancel),
		nopClose(eng.wg.Wait),
		nopClose(func() { close(eng.snapshotc) }),
		nopClose(eng.node.Stop),
		eng.msgbus.Close,
		// eng.storage.Close,
		func() error {
			return eng.pool.TearDown(ctx)
		},
	}

	for _, fn := range fns {
		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}

// TransferLeadership attempts to transfer leadership to the given transferee.
func (eng *engine) TransferLeadership(ctx context.Context, transferee uint64) error {
	if eng.started.False() {
		return ErrStopped
	}

	eng.propwg.Add(1)
	defer eng.propwg.Done()

	eng.logger.Infof("raft.engine: start transfer leadership %x -> %x", eng.node.Status().Lead, transferee)

	eng.node.TransferLeadership(ctx, eng.node.Status().Lead, transferee)
	ticker := time.NewTicker(eng.cfg.TickInterval() / 10)
	defer ticker.Stop()
	for {
		leader := eng.node.Status().Lead
		if leader != raft.None && leader == transferee {
			break
		}
		select {
		case <-eng.ctx.Done():
			return ErrStopped
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}

	return nil
}

// ProposeReplicate proposes to replicate the data to be appended to the raft eng.logger.
func (eng *engine) ProposeReplicate(ctx context.Context, data []byte) error {
	ctx, tr := trace.NewTask(ctx, "raft.engine.ProposeReplicate")
	defer tr.End()

	if eng.started.False() {
		return ErrStopped
	}

	eng.propwg.Add(1)
	defer eng.propwg.Done()

	r := &raftpb.Replicate{
		CID:  eng.idgen.Next(),
		Data: data,
	}

	buf, err := r.Marshal()
	if err != nil {
		return err
	}

	eng.logger.V(1).Infof("raft.engine: propose replicate data, change id => %d", r.CID)

	if err := eng.node.Propose(ctx, buf); err != nil {
		return err
	}

	// wait for changes to be done
	return eng.wait(ctx, r.CID)
}

// ProposeConfChange proposes a configuration change to the cluster pool members.
func (eng *engine) ProposeConfChange(ctx context.Context, m *raftpb.Member, cct etcdraftpb.ConfChangeType) error {
	if eng.started.False() {
		return ErrStopped
	}

	eng.propwg.Add(1)
	defer eng.propwg.Done()

	id, err := eng.proposeConfChange(ctx, m, cct)
	if err != nil {
		return err
	}

	// wait for changes to be done
	return eng.wait(ctx, id)
}

func (eng *engine) ForgetLeader(ctx context.Context) error {
	return eng.node.ForgetLeader(ctx)
}

// CreateSnapshot begin a snapshot and return snap metadata.
func (eng *engine) CreateSnapshot() (etcdraftpb.Snapshot, error) {
	if eng.started.False() {
		return etcdraftpb.Snapshot{}, ErrStopped
	}

	appliedIndex := eng.appliedIndex.Get()
	snapIndex := eng.snapIndex.Get()

	if appliedIndex == snapIndex {
		// up to date just return the latest snap to load it from disk.
		// return eng.storage.Snapshot()
		return eng.storage.Snapshot()
	}

	c := make(chan error)
	eng.snapshotc <- c
	if err := <-c; err != nil {
		return etcdraftpb.Snapshot{}, err
	}

	// return eng.storage.Snapshot()
	return eng.storage.Snapshot()
}

// Start engine.
func (eng *engine) Start(ctx context.Context, addr string, oprs ...Operator) error {
	ctx, t := trace.NewTask(ctx, "raft.Start")
	defer t.End()

	sp := setup{addr: addr}
	ssp := stateSetup{publishSnapshotFile: eng.publishSnapshotFile}
	rm := removedMembers{}
	oprs = append(oprs, sp, ssp, rm)
	ost, err := invoke(ctx, eng, oprs...)
	if err != nil {
		return err
	}

	if eng.node == nil {
		return errors.New("raft: node not initialized, use raft.WithInitCluster() or raft.WithRestart()")
	}

	// set local member.
	eng.local = ost.local
	eng.idgen = idutil.NewGenerator(uint16(eng.local.ID), time.Now())
	eng.ctx, eng.cancel = context.WithCancel(ctx)
	eng.proposec = make(chan etcdraftpb.Message, 4096)
	eng.msgc = make(chan etcdraftpb.Message, 4096)
	eng.snapshotc = make(chan chan error)
	eng.appendc = make(chan etcdraftpb.Message, 4094)
	eng.applyc = make(chan etcdraftpb.Message, 4094)
	eng.started.Set()

	eng.process(ctx, eng.proposec)
	eng.process(ctx, eng.msgc)
	return eng.eventLoop(ctx)
	// return eng.asyncEventLoop(ctx)
}

func (eng *engine) eventLoop(ctx context.Context) error {
	eng.wg.Add(1)
	defer eng.wg.Done()

	ticker := time.NewTicker(eng.cfg.TickInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			eng.node.Tick()
		case rd := <-eng.node.Ready():
			if err := eng.do(ctx, rd); err != nil {
				return err
			}
		case c := <-eng.snapshotc:
			// c <- eng.createSnapshot(ctx)
			c <- nil
		case <-eng.ctx.Done():
			return ErrStopped
		}
	}
}

func (eng *engine) do(ctx context.Context, rd raft.Ready) error {
	ctx, tr := trace.NewTask(ctx, "raft.engine.do")
	defer tr.End()

	prevIndex := eng.appliedIndex.Get()

	if rd.SoftState != nil {
		eng.leader = rd.RaftState == raft.StateLeader
		if rd.SoftState.Lead == raft.None {
			eng.msgbus.BroadcastToAll(ErrNoLeader)
		}
		go func() {
			if eng.statec != nil {
				select {
				case <-eng.statec:
				case eng.statec <- rd.SoftState.RaftState:
				default:
				}
			}
		}()
	}

	var wg sync.WaitGroup
	if eng.leader {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eng.send(ctx, rd.Messages)
		}()
	}

	if err := eng.saveEntries(ctx, &rd.HardState, rd.Entries); err != nil {
		return err
	}

	if err := eng.publishSnapshot(ctx, &rd.Snapshot); err != nil {
		return err
	}

	// if err := eng.cache.Append(rd.Entries); err != nil {
	// 	return err
	// }

	eng.publishCommitted(ctx, rd.CommittedEntries)
	eng.publishReadState(ctx, rd.ReadStates)
	eng.publishAppliedIndices(ctx, prevIndex, eng.appliedIndex.Get())
	eng.promotions(ctx)
	eng.maybeCreateSnapshot(ctx)
	if !eng.leader {
		eng.send(ctx, rd.Messages)
	} else {
		wg.Wait()
	}
	eng.node.Advance()
	return nil
}

func (eng *engine) saveEntries(ctx context.Context, hs *etcdraftpb.HardState, es []etcdraftpb.Entry) error {
	ctx, tr := trace.NewTask(ctx, "raft.engine.saveEntries")
	defer tr.End()
	if err := eng.storage.SaveEntries(ctx, hs, es); err != nil {
		return err
	}
	return nil
}

func (eng *engine) asyncEventLoop(ctx context.Context) error {
	eng.wg.Add(1)
	defer eng.wg.Done()

	ticker := time.NewTicker(eng.cfg.TickInterval())
	defer ticker.Stop()

	errs := make(chan error, 2)

	go eng.appendLoop(ctx, errs)
	go eng.applyLoop(ctx, errs)

	for {
		select {
		case <-ticker.C:
			eng.node.Tick()
		case rd := <-eng.node.Ready():
			for _, v := range rd.Messages {
				switch v.Type {
				case etcdraftpb.MsgStorageAppend:
					eng.appendc <- v
				case etcdraftpb.MsgStorageApply:
					eng.applyc <- v
				default:
					eng.send(ctx, []etcdraftpb.Message{v})
				}
			}
			eng.publishReadState(ctx, rd.ReadStates)
			if rd.SoftState != nil {
				eng.leader = rd.RaftState == raft.StateLeader
				if rd.SoftState.Lead == raft.None {
					eng.msgbus.BroadcastToAll(ErrNoLeader)
				}
				go func() {
					if eng.statec != nil {
						select {
						case <-eng.statec:
						case eng.statec <- rd.SoftState.RaftState:
						default:
						}
					}
				}()
			}
		case c := <-eng.snapshotc:
			c <- eng.createSnapshot(ctx)
		case <-eng.ctx.Done():
			return ErrStopped
		}
	}
}

func (eng *engine) appendLoop(ctx context.Context, errs chan error) {
	eng.wg.Add(1)
	defer eng.wg.Done()
	errs <- func() error {
		for {
			select {
			case m := <-eng.appendc:
				hs := etcdraftpb.HardState{
					Term:   m.Term,
					Vote:   m.Vote,
					Commit: m.Commit,
				}
				if err := eng.saveEntries(ctx, &hs, m.Entries); err != nil {
					return err
				}
				eng.send(ctx, m.Responses)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}()
}

func (eng *engine) applyLoop(ctx context.Context, errs chan error) {
	eng.wg.Add(1)
	defer eng.wg.Done()
	errs <- func() error {
		for {
			select {
			case m := <-eng.applyc:
				prevIndex := eng.appliedIndex.Get()
				eng.publishCommitted(ctx, m.Entries)
				eng.publishAppliedIndices(ctx, prevIndex, eng.appliedIndex.Get())
				eng.promotions(ctx)
				eng.maybeCreateSnapshot(ctx)
				eng.send(ctx, m.Responses)
			case <-ctx.Done():
				return nil
			}
		}
	}()
}

// func (eng *engine) cacheEntries(ctx context.Context, entries []etcdraftpb.Entry) error {
// 	ctx, tr := trace.NewTask(ctx, "raft.engine.cacheEntries")
// 	defer tr.End()
//
// 	return eng.cache.Append(entries)
// }

func (eng *engine) proposeConfChange(
	ctx context.Context,
	m *raftpb.Member,
	t etcdraftpb.ConfChangeType,
) (uint64, error) {

	buf, err := m.Marshal()
	if err != nil {
		return 0, err
	}

	cc := etcdraftpb.ConfChange{
		ID:      eng.idgen.Next(),
		Type:    t,
		NodeID:  m.ID,
		Context: buf,
	}

	eng.logger.V(1).Infof("raft.engine: propose conf change, change id => %d", cc.ID)
	return cc.ID, eng.node.ProposeConfChange(ctx, cc)
}

func (eng *engine) publishReadState(ctx context.Context, rss []raft.ReadState) {
	ctx, t := trace.NewTask(ctx, "raft.engine.publishReadState")
	defer t.End()
	for _, rs := range rss {
		id := binary.BigEndian.Uint64(rs.RequestCtx)
		eng.msgbus.Broadcast(id, rs.Index)
	}
}

func (eng *engine) publishAppliedIndices(ctx context.Context, prev, curr uint64) {
	ctx, tr := trace.NewTask(ctx, "raft.engine.publishAppliedIndices")
	defer tr.End()

	for i := prev + 1; i < curr+1; i++ {
		eng.msgbus.Broadcast(i, nil)
	}
}

func (eng *engine) publishSnapshot(ctx context.Context, snap *etcdraftpb.Snapshot) error {
	ctx, t := trace.NewTask(ctx, "raft.engine.publishSnapshot")
	defer t.End()
	if raft.IsEmptySnap(*snap) {
		return nil
	}

	if snap.Metadata.Index <= eng.appliedIndex.Get() {
		return fmt.Errorf(
			"raft: snapshot index [%d] should > progress.appliedIndex [%s]",
			snap.Metadata.Index,
			eng.appliedIndex,
		)
	}

	if err := eng.storage.SaveSnapshot(ctx, snap); err != nil {
		return err
	}

	meta := snap.Metadata
	sf, err := eng.storage.Snapshotter().Read(meta.Term, meta.Index)
	if err != nil {
		return err
	}

	return eng.publishSnapshotFile(ctx, sf)
}

func (eng *engine) publishSnapshotFile(ctx context.Context, sf *storage.Snapshot) error {
	ctx, t := trace.NewTask(ctx, "raft.engine.publishSnapshotFile")
	defer t.End()
	snap := sf.Raw

	if err := eng.storage.ApplySnapshot(snap); err != nil {
		return err
	}

	eng.pool.Restore(ctx, sf.Members)

	if err := eng.fsm.Restore(sf.Data); err != nil {
		return err
	}

	eng.confState = &snap.Metadata.ConfState
	eng.snapIndex.Set(snap.Metadata.Index)
	eng.appliedIndex.Set(snap.Metadata.Index)
	return nil
}

func (eng *engine) publishCommitted(ctx context.Context, ents []etcdraftpb.Entry) {
	ctx, t := trace.NewTask(ctx, "raft.engine.publishCommitted")
	defer t.End()
	for _, ent := range ents {
		if ent.Type == etcdraftpb.EntryNormal && len(ent.Data) > 0 {
			eng.publishReplicate(ctx, ent)
		}
		if ent.Type == etcdraftpb.EntryConfChange {
			eng.publishConfChange(ctx, ent)
		}
		eng.appliedIndex.Set(ent.Index)
	}
}

func (eng *engine) publishReplicate(ctx context.Context, ent etcdraftpb.Entry) {
	ctx, t := trace.NewTask(ctx, "raft.engine.publishReplicate")
	defer t.End()
	var err error
	r := new(raftpb.Replicate)
	defer func() {
		eng.msgbus.Broadcast(r.CID, err)
		if err != nil {
			eng.logger.Warningf(
				"raft.engine: publishing replicate data: %v",
				err,
			)
		}
	}()

	if err = r.Unmarshal(ent.Data); err != nil {
		return
	}

	eng.logger.V(1).Infof("raft.engine: publishing replicate data, change id => %d", r.CID)

	err = eng.fsm.Apply(r.Data)
	return
}

func (eng *engine) publishConfChange(ctx context.Context, ent etcdraftpb.Entry) {
	ctx, t := trace.NewTask(ctx, "raft.engine.publishConfChange")
	defer t.End()
	var err error
	cc := new(etcdraftpb.ConfChange)
	mem := new(raftpb.Member)

	defer func() {
		eng.msgbus.Broadcast(cc.ID, err)
		if err != nil {
			eng.logger.Warningf("raft.engine: publishing conf change: %v", err)
		}
	}()

	if err = cc.Unmarshal(ent.Data); err != nil {
		return
	}

	eng.logger.V(1).Infof("raft.engine: publishing conf change, change id => %d", cc.ID)

	if len(cc.Context) == 0 {
		return
	}

	if err = mem.Unmarshal(cc.Context); err != nil {
		return
	}

	switch cc.Type {
	case etcdraftpb.ConfChangeAddNode, etcdraftpb.ConfChangeAddLearnerNode:
		err = eng.pool.Add(ctx, *mem)
	case etcdraftpb.ConfChangeUpdateNode:
		err = eng.pool.Update(ctx, *mem)
	case etcdraftpb.ConfChangeRemoveNode:
		eng.wg.Add(1)
		go func(mem raftpb.Member) {
			defer eng.wg.Done()
			select {
			// wait for two ticks then go and remove the member from the pool.
			// to make sure the commit ack sent before closing connection.
			case <-time.After(eng.cfg.TickInterval() * 2):
				if err := eng.pool.Remove(ctx, mem); err != nil {
					eng.logger.Errorf("raft.engine: removing member %x: %v", mem.ID, err)
				}
			case <-ctx.Done():
				return
			}
		}(*mem)
	}

	eng.confState = eng.node.ApplyConfChange(cc)
}

// process the incoming messages from the given chan.
func (eng *engine) process(ctx context.Context, c chan etcdraftpb.Message) {
	ctx, t := trace.NewTask(ctx, "raft.engine.process")
	defer t.End()

	eng.processwg.Add(1)
	go func() {
		defer eng.processwg.Done()
		// process must keep processing msg until c closed or ctx.Done(),
		// for graceful shutdown purposes.
		for m := range c {
			if err := ctx.Err(); err != nil {
				return
			}

			if err := eng.node.Step(ctx, m); err != nil {
				eng.logger.Warningf("raft.engine: process raft message: %v", err)
			}
		}
	}()
}

func (eng *engine) send(ctx context.Context, msgs []etcdraftpb.Message) {
	ctx, tr := trace.NewTask(ctx, "raft.engine.send")
	defer tr.End()

	lg := func(m etcdraftpb.Message, str string) {
		eng.logger.Warningf(
			"raft.engine: sending message %s to member %x: %v",
			m.Type,
			m.To,
			str,
		)
	}

	for _, m := range msgs {
		if m.To == eng.local.ID {
			if err := eng.node.Step(ctx, m); err != nil {
				lg(m, err.Error())
			}
			continue
		}
		mem, ok := eng.pool.Get(ctx, m.To)
		if !ok {
			lg(m, "unknown member")
			continue
		}

		if eng.forceSnapshot(ctx, m) {
			continue
		}

		if err := mem.Send(m); err != nil {
			lg(m, err.Error())
		}
	}
}

func (eng *engine) promotions(ctx context.Context) {
	ctx, tr := trace.NewTask(ctx, "raft.engine.promotions")
	defer tr.End()

	rs := eng.node.Status()

	// the current node is not the leader.
	if rs.Progress == nil {
		return
	}

	promotions := []raftpb.Member{}
	membs := eng.pool.Members()
	reachables := 0
	voters := 0

	for _, mem := range membs {
		raw := mem.Raw()
		if raw.Type == raftpb.VoterMember {
			voters++
		}

		if mem.IsActive() && raw.Type == raftpb.VoterMember {
			reachables++
		}

		if raw.Type != raftpb.StagingMember {
			continue
		}

		leader := rs.Progress[rs.ID].Match
		staging := rs.Progress[raw.ID].Match

		// the staging Match not caught up with the leader yet.
		if float64(staging) < float64(leader)*0.9 {
			continue
		}

		(&raw).Type = raftpb.VoterMember
		promotions = append(promotions, raw)
	}

	// quorum lost and the cluster unavailable, no new logs can be committed.
	if reachables < voters/2+1 {
		return
	}

	for _, m := range promotions {
		eng.logger.Infof("raft.engine: promoting staging member %x", m.ID)
		ctx, cancel := context.WithTimeout(ctx, eng.cfg.TickInterval()*5)
		_, err := eng.proposeConfChange(ctx, &m, etcdraftpb.ConfChangeAddNode)
		if err != nil {
			eng.logger.Warningf("raft.engine: promoting staging member %x: %v", m.ID, err)
		}
		cancel()
	}
}

func (eng *engine) forceSnapshot(ctx context.Context, msg etcdraftpb.Message) bool {
	ctx, tr := trace.NewTask(ctx, "raft.engine.forceSnapshot")
	defer tr.End()

	if msg.Type != etcdraftpb.MsgSnap {
		return false
	}

	found := false
	cs := msg.Snapshot.Metadata.ConfState
	for _, set := range [][]uint64{
		cs.Voters,
		cs.Learners,
		cs.VotersOutgoing,
		// `LearnersNext` doesn't need to be checked. According to the rules, if a peer in
		// `LearnersNext`, it has to be in `VotersOutgoing`.
	} {

		for _, id := range set {
			if id == msg.To {
				found = true
				break
			}
		}

		if found {
			break
		}
	}

	if found {
		return false
	}

	eng.logger.V(1).Infof("raft.engine: force new snapshot %x it is not in the ConfState", msg.To)

	// report snapshot failure, to re-send the new snapshot.
	defer eng.ReportSnapshot(msg.To, raft.SnapshotFailure)

	if err := eng.createSnapshot(ctx); err != nil {
		eng.logger.Warningf("raft.engine: force new snapshot: %v", err)
	}

	return true
}

func (eng *engine) maybeCreateSnapshot(ctx context.Context) {
	ctx, tr := trace.NewTask(ctx, "raft.engine.maybeCreateSnapshot")
	defer tr.End()

	if eng.appliedIndex.Get()-eng.snapIndex.Get() <= eng.cfg.SnapInterval() || eng.snapshoting.True() {
		return
	}

	if err := eng.createSnapshot(ctx); err != nil {
		if errors.Is(err, ErrFailedPrecondition) {
			return
		}
		eng.logger.Errorf(
			"raft.engine: creating new snapshot at index %s failed: %v",
			eng.appliedIndex,
			err,
		)
	}
}

func (eng *engine) createSnapshot(ctx context.Context) error {
	ctx, tr := trace.NewTask(ctx, "raft.engine.createSnapshot")
	defer tr.End()

	appliedIndex := eng.appliedIndex.Get()
	snapIndex := eng.snapIndex.Get()

	if appliedIndex == snapIndex {
		return nil
	}

	if eng.snapshoting.True() {
		return ErrAlreadySnapshotting
	}

	eng.snapshoting.Set()

	r, err := eng.fsm.Snapshot()
	if err != nil {
		eng.snapshoting.UnSet()
		return err
	}

	eng.logger.Infof(
		"raft.engine: start snapshot [applied index: %d | last snapshot index: %d]",
		appliedIndex,
		snapIndex,
	)

	snap, err := eng.storage.CreateSnapshot(ctx, appliedIndex, eng.confState, nil)
	if err != nil {
		eng.snapshoting.UnSet()
		return err
	}

	ss := storage.Snapshot{
		SnapshotState: raftpb.SnapshotState{
			Raw:     snap,
			Members: eng.pool.Snapshot(ctx),
		},
		Data: r,
	}

	fn := func() error {
		defer eng.snapshoting.UnSet()

		if err := eng.storage.Snapshotter().Write(&ss); err != nil {
			return err
		}

		if err := eng.storage.SaveSnapshot(ctx, &snap); err != nil {
			return err
		}

		eng.snapIndex.Set(appliedIndex)

		if appliedIndex <= eng.cfg.SnapInterval() {
			return nil
		}

		compactIndex := appliedIndex - eng.cfg.SnapInterval()
		if err := eng.storage.Compact(compactIndex); err != nil {
			return err
		}

		eng.logger.Infof("raft.engine: compacted log at index %d", compactIndex)
		return nil
	}

	eng.wg.Add(1)
	go func() {
		defer eng.wg.Done()
		if err := fn(); err != nil {
			eng.snapIndex.Set(snapIndex)
			eng.logger.Errorf(
				"raft.engine: creating new snapshot at index %s failed: %v",
				eng.appliedIndex,
				err,
			)
		}
	}()
	return nil
}

func (eng *engine) wait(ctx context.Context, id uint64) error {
	ctx, tr := trace.NewTask(ctx, "raft.engine.wait")
	defer tr.End()

	sub := eng.msgbus.SubscribeOnce(id)
	defer sub.Unsubscribe()

	select {
	case v := <-sub.Chan():
		if v != nil {
			return v.(error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-eng.ctx.Done():
		return ErrStopped
	}
}

func nopClose(fn func()) func() error {
	return func() error {
		fn()
		return nil
	}
}
